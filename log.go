package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/term"
)

const (
	logMagic    = "PCGL"
	logPageSize = 10
)

func logDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".passcode-gen"), nil
}

// loadOrCreateSalt は ~/.passcode-gen/.salt を読み込む。存在しない場合は 32 バイトの
// ランダムソルトを生成して保存する。
func loadOrCreateSalt() ([]byte, error) {
	dir, err := logDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	saltPath := filepath.Join(dir, ".salt")
	data, err := os.ReadFile(saltPath)
	if err == nil && len(data) == 32 {
		return data, nil
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if err := os.WriteFile(saltPath, salt, 0600); err != nil {
		return nil, err
	}
	return salt, nil
}

// deriveKey は実行バイナリの SHA-256 と salt を組み合わせて AES-256 鍵を導出する。
// 別ビルドのバイナリでは異なる鍵になるため、ログファイルを復号できない。
func deriveKey(salt []byte) ([]byte, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("実行ファイルのパスを取得できません: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return nil, fmt.Errorf("シンボリックリンクを解決できません: %w", err)
	}
	f, err := os.Open(realPath)
	if err != nil {
		return nil, fmt.Errorf("実行ファイルを開けません: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("バイナリのハッシュ計算に失敗しました: %w", err)
	}
	binaryHash := h.Sum(nil)

	mac := hmac.New(sha256.New, binaryHash)
	mac.Write(salt)
	return mac.Sum(nil), nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// firstRecordDecryptable は data の先頭レコードを aead で復号できるかを返す。
// レコードが存在しない（magic のみ or 空）場合は互換とみなし true を返す。
func firstRecordDecryptable(data []byte, aead cipher.AEAD) bool {
	if len(data) <= 4 {
		return true
	}
	if string(data[:4]) != logMagic {
		return false
	}
	nonceSize := aead.NonceSize()
	pos := 4
	if pos+nonceSize+4 > len(data) {
		return true
	}
	nonce := data[pos : pos+nonceSize]
	pos += nonceSize
	size := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	if pos+size > len(data) {
		return false
	}
	_, err := aead.Open(nil, nonce, data[pos:pos+size], nil)
	return err == nil
}

// appendLogs は各 PIN を "timestamp PIN" 形式で暗号化してログファイルに追記する。
// 既存ログが現在の鍵で復号できない場合（バイナリ更新など）はリセットしてから追記する。
// ログファイル形式: [magic 4bytes][records...]
// 各レコード: [nonce 12bytes][size 4bytes big-endian][ciphertext size bytes]
func appendLogs(pins []string) error {
	salt, err := loadOrCreateSalt()
	if err != nil {
		return err
	}
	key, err := deriveKey(salt)
	if err != nil {
		return err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}

	dir, err := logDir()
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "log.bin")

	// 既存ログが現在の鍵で読めない場合はリセット（上書きモードに切り替え）
	openFlags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	if existing, readErr := os.ReadFile(logPath); readErr == nil && !firstRecordDecryptable(existing, aead) {
		openFlags = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
		fmt.Fprintln(os.Stderr, "警告: バイナリの更新を検出しました。過去のログをリセットして新規保存します。")
	}

	f, err := os.OpenFile(logPath, openFlags, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		if _, err := f.Write([]byte(logMagic)); err != nil {
			return err
		}
	}

	now := time.Now().Format(time.RFC3339)
	for _, pin := range pins {
		plaintext := []byte(now + " " + pin)
		nonce := make([]byte, aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		ciphertext := aead.Seal(nil, nonce, plaintext, nil)

		if _, err := f.Write(nonce); err != nil {
			return err
		}
		var sizeBuf [4]byte
		binary.BigEndian.PutUint32(sizeBuf[:], uint32(len(ciphertext)))
		if _, err := f.Write(sizeBuf[:]); err != nil {
			return err
		}
		if _, err := f.Write(ciphertext); err != nil {
			return err
		}
	}
	return nil
}

// readLogs はログファイルを読み込み、全レコードを復号して返す（古い順）。
func readLogs() ([]string, error) {
	salt, err := loadOrCreateSalt()
	if err != nil {
		return nil, err
	}
	key, err := deriveKey(salt)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}

	dir, err := logDir()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "log.bin")

	data, err := os.ReadFile(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if len(data) < 4 || string(data[:4]) != logMagic {
		_ = os.Remove(logPath)
		fmt.Fprintln(os.Stderr, "警告: ログファイルが不正な形式でした。リセットしました。")
		return nil, nil
	}

	nonceSize := aead.NonceSize()
	pos := 4
	var entries []string

	resetCorrupt := func() ([]string, error) {
		_ = os.Remove(logPath)
		fmt.Fprintln(os.Stderr, "警告: ログファイルが破損していました。リセットしました。")
		return nil, nil
	}

	for pos < len(data) {
		if pos+nonceSize+4 > len(data) {
			return resetCorrupt()
		}
		nonce := data[pos : pos+nonceSize]
		pos += nonceSize
		size := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if pos+size > len(data) {
			return resetCorrupt()
		}
		ciphertext := data[pos : pos+size]
		pos += size

		plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			_ = os.Remove(logPath)
			fmt.Fprintln(os.Stderr, "警告: ログの復号に失敗しました（バイナリが更新された場合は過去ログを読めません）。リセットしました。")
			return nil, nil
		}
		entries = append(entries, string(plaintext))
	}
	return entries, nil
}

// runLogView はログを新しい順に表示する。
// 10件以下はそのまま stdout へ出力し、11件以上はインタラクティブ TUI で
// 矢印キーによるスクロールを提供する。q/ESC/Ctrl+C で終了する。
func runLogView() error {
	entries, err := readLogs()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("ログがありません。")
		return nil
	}

	n := len(entries)
	reversed := make([]string, n)
	for i, e := range entries {
		reversed[n-1-i] = e
	}

	// 10件以下はインタラクティブ TUI 不要
	if n <= logPageSize {
		for _, e := range reversed {
			fmt.Println(e)
		}
		return nil
	}

	// 11件以上: raw モード TUI でスクロール表示
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return fmt.Errorf("ターミナルを開けません: %w", err)
	}
	defer tty.Close()

	fd := int(tty.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("ターミナルをrawモードにできません: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	start := 0

	render := func() {
		fmt.Print("\033[H\033[J")
		end := start + logPageSize
		if end > n {
			end = n
		}
		for _, e := range reversed[start:end] {
			fmt.Printf("%s\r\n", e)
		}
		fmt.Printf("\r\n[↑↓] スクロール  [q / ESC / Ctrl+C] 終了  (%d-%d / %d件)\r\n", start+1, end, n)
	}

	render()

	buf := make([]byte, 3)
	for {
		nr, err := tty.Read(buf)
		if err != nil {
			return err
		}
		switch {
		case nr == 1 && (buf[0] == 0x1b || buf[0] == 0x03 || buf[0] == 'q' || buf[0] == 'Q'):
			fmt.Print("\033[H\033[J")
			return nil
		case nr == 3 && buf[0] == 0x1b && buf[1] == '[':
			switch buf[2] {
			case 'A': // ↑ 新しい方向
				if start > 0 {
					start--
					render()
				}
			case 'B': // ↓ 古い方向
				if start+logPageSize < n {
					start++
					render()
				}
			}
		}
	}
}
