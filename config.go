package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const configMagic = "PCGC"

// ---- データ型 ----

type Config struct {
	Salt           []byte      `json:"salt"`
	DelaySeconds   int         `json:"delay_seconds,omitempty"`
	Schedules      []Schedule  `json:"schedules,omitempty"`
	PendingView    *PendingReq `json:"pending_view,omitempty"`
	PendingConfig  *PendingCfg `json:"pending_config,omitempty"`
	PendingDisable *PendingReq `json:"pending_disable,omitempty"`
}

type Schedule struct {
	Days  []int  `json:"days"`  // Go の time.Weekday 値: 0=Sun,...,6=Sat
	Start string `json:"start"` // "09:00"
	End   string `json:"end"`   // "11:00"
}

type PendingReq struct {
	RequestedAt  time.Time `json:"requested_at"`
	DelaySeconds int       `json:"delay_seconds"` // 要求時点の遅延秒数（タイマー評価基準）
}

type PendingCfg struct {
	RequestedAt     time.Time  `json:"requested_at"`
	WaitSeconds     int        `json:"wait_seconds"`            // 待機秒数（要求時の DelaySeconds）
	NewDelaySeconds int        `json:"new_delay_seconds"`       // 適用する新しい遅延秒数
	NewSchedules    []Schedule `json:"new_schedules,omitempty"` // 適用する新しいスケジュール
}

// newAEAD は key から AES-256-GCM の AEAD を生成する。
func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// ---- バイナリハッシュ・鍵導出 ----

// binaryHash は実行バイナリの SHA-256 を返す。
func binaryHash() ([]byte, error) {
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
	return h.Sum(nil), nil
}

// deriveConfigKey は configSalt とバイナリハッシュから config 暗号化鍵を導出する。
// configSalt はファイル作成時にランダム生成して平文保存するため、鍵にランダム性が確保される。
func deriveConfigKey(configSalt []byte) ([]byte, error) {
	hash, err := binaryHash()
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, hash)
	mac.Write(configSalt)
	return mac.Sum(nil), nil
}

// logKeyFromConfig は Config.Salt とバイナリハッシュから log.bin 暗号化鍵を導出する。
func logKeyFromConfig(c *Config) ([]byte, error) {
	hash, err := binaryHash()
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, hash)
	mac.Write(c.Salt)
	return mac.Sum(nil), nil
}

// ---- config.bin I/O ----

// loadConfig は config.bin を読み込む。
// config.bin が存在せず .salt があれば config.bin へ移行する（書き込み→削除の順）。
// どちらも存在しない場合（初回）は nil, nil を返す。
func loadConfig() (*Config, error) {
	dir, err := logDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	configPath := filepath.Join(dir, "config.bin")
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		// .salt があれば config.bin へ移行
		saltPath := filepath.Join(dir, ".salt")
		saltData, saltErr := os.ReadFile(saltPath)
		if saltErr == nil && len(saltData) == 32 {
			c := &Config{Salt: saltData}
			if saveErr := saveConfig(c); saveErr != nil {
				return nil, fmt.Errorf(".salt の移行に失敗しました: %w", saveErr)
			}
			_ = os.Remove(saltPath)
			return c, nil
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// config.bin が壊れている・現在の鍵で復号できない場合は自動でリセットする
	// （log.bin の readLogs と同じ方針。バイナリ更新時に鍵が変わることが主な原因）。
	resetCorrupt := func(reason string) (*Config, error) {
		_ = os.Remove(configPath)
		fmt.Fprintln(os.Stderr, "警告: "+reason+" リセットしました。")
		return nil, nil
	}

	// フォーマット: [magic 4B][configSalt 32B][nonce 12B][size 4B][ciphertext]
	if len(data) < 4+32 || string(data[:4]) != configMagic {
		return resetCorrupt("config.bin のフォーマットが不正でした。")
	}
	configSalt := data[4:36]
	pos := 36

	key, err := deriveConfigKey(configSalt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := aead.NonceSize()
	if pos+nonceSize+4 > len(data) {
		return resetCorrupt("config.bin が破損していました（ヘッダが短い）。")
	}
	nonce := data[pos : pos+nonceSize]
	pos += nonceSize
	size := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	if pos+size > len(data) {
		return resetCorrupt("config.bin が破損していました（サイズ不正）。")
	}

	plaintext, err := aead.Open(nil, nonce, data[pos:pos+size], nil)
	if err != nil {
		return resetCorrupt("config.bin の復号に失敗しました（バイナリが更新された可能性があります）。")
	}

	var c Config
	if err := json.Unmarshal(plaintext, &c); err != nil {
		return resetCorrupt("config.bin の解析に失敗しました。")
	}
	return &c, nil
}

// saveConfig は Config を config.bin に AES-256-GCM で暗号化保存する。
// 保存のたびに configSalt を新規生成するため、毎回異なる鍵になる。
func saveConfig(c *Config) error {
	dir, err := logDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	configSalt := make([]byte, 32)
	if _, err := rand.Read(configSalt); err != nil {
		return err
	}
	key, err := deriveConfigKey(configSalt)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	plaintext, err := json.Marshal(c)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	buf := make([]byte, 0, 4+32+aead.NonceSize()+4+len(ciphertext))
	buf = append(buf, []byte(configMagic)...)
	buf = append(buf, configSalt...)
	buf = append(buf, nonce...)
	var sizeBuf [4]byte
	binary.BigEndian.PutUint32(sizeBuf[:], uint32(len(ciphertext)))
	buf = append(buf, sizeBuf[:]...)
	buf = append(buf, ciphertext...)

	return os.WriteFile(filepath.Join(dir, "config.bin"), buf, 0600)
}

// ---- スケジュールパース ----

// jaWeekday は Go の time.Weekday 値（0=日,1=月,...,6=土）に対応する日本語曜日名。
var jaWeekday = []string{"日", "月", "火", "水", "木", "金", "土"}

// 内部順序: 月=0,火=1,...,土=5,日=6（レンジ展開用。日曜を末尾に置くことで「土-日」が自然に展開できる）
var jaDayToInternal = map[string]int{"月": 0, "火": 1, "水": 2, "木": 3, "金": 4, "土": 5, "日": 6}
var enDayToInternal = map[string]int{"Mon": 0, "Tue": 1, "Wed": 2, "Thu": 3, "Fri": 4, "Sat": 5, "Sun": 6}

// internalToGoWeekday は内部インデックス → Go の time.Weekday 値への変換表。
var internalToGoWeekday = []int{1, 2, 3, 4, 5, 6, 0} // 月→1,...,土→6,日→0

func dayNameToInternal(name string) (int, bool) {
	if v, ok := jaDayToInternal[name]; ok {
		return v, true
	}
	if v, ok := enDayToInternal[name]; ok {
		return v, true
	}
	return 0, false
}

// expandDayRange は内部インデックスの [start, end] を Go Weekday 値の slice に展開する。
// 「土-日」(5-6) のように end >= start でない場合も正しく周回する。
func expandDayRange(start, end int) []int {
	var days []int
	i := start
	for {
		days = append(days, internalToGoWeekday[i])
		i = (i + 1) % 7
		if i == (end+1)%7 {
			break
		}
	}
	return days
}

func parseDayToken(token string) ([]int, error) {
	if dashIdx := strings.Index(token, "-"); dashIdx > 0 {
		startName, endName := token[:dashIdx], token[dashIdx+1:]
		si, ok1 := dayNameToInternal(startName)
		ei, ok2 := dayNameToInternal(endName)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("不明な曜日: %q", token)
		}
		return expandDayRange(si, ei), nil
	}
	internal, ok := dayNameToInternal(token)
	if !ok {
		return nil, fmt.Errorf("不明な曜日: %q", token)
	}
	return []int{internalToGoWeekday[internal]}, nil
}

// parseSchedule は "月-金 20:00-22:00" 形式の文字列を Schedule に変換する。
// 曜日は英語（Mon-Sun）・日本語（月-日）どちらも受け付ける。
// カンマ区切り（"土,日"）とレンジ（"月-金"）は混在可能。
func parseSchedule(s string) (Schedule, error) {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return Schedule{}, fmt.Errorf("スケジュール書式が不正です: %q (例: \"月-金 20:00-22:00\")", s)
	}
	daysPart, timePart := fields[0], fields[1]

	tidx := strings.Index(timePart, "-")
	if tidx < 0 {
		return Schedule{}, fmt.Errorf("時刻範囲の書式が不正です: %q", timePart)
	}
	startTime, endTime := timePart[:tidx], timePart[tidx+1:]
	if !isValidHHMM(startTime) || !isValidHHMM(endTime) {
		return Schedule{}, fmt.Errorf("時刻の書式が不正です: %q", timePart)
	}

	var allDays []int
	seen := make(map[int]bool)
	for _, token := range strings.Split(daysPart, ",") {
		days, err := parseDayToken(strings.TrimSpace(token))
		if err != nil {
			return Schedule{}, err
		}
		for _, d := range days {
			if !seen[d] {
				seen[d] = true
				allDays = append(allDays, d)
			}
		}
	}
	if len(allDays) == 0 {
		return Schedule{}, fmt.Errorf("曜日が指定されていません: %q", daysPart)
	}
	return Schedule{Days: allDays, Start: startTime, End: endTime}, nil
}

func isValidHHMM(t string) bool {
	if len(t) != 5 || t[2] != ':' {
		return false
	}
	h, m := t[:2], t[3:]
	for _, c := range h + m {
		if c < '0' || c > '9' {
			return false
		}
	}
	return h >= "00" && h <= "23" && m >= "00" && m <= "59"
}

// ---- スケジュール判定 ----

// inSchedule は t がいずれかのスケジュールスロット内に含まれるかを返す。
// Start > End の場合は日をまたぐ夜間スケジュール（例: 22:00-02:00）とみなし、
// 前日の Days に登録された曜日の夜から翌日の早朝までを対象とする。
func inSchedule(schedules []Schedule, t time.Time) bool {
	weekday := int(t.Weekday())
	prevWeekday := (weekday + 6) % 7
	hhmm := t.Format("15:04")
	for _, s := range schedules {
		overnight := s.Start > s.End
		for _, d := range s.Days {
			if !overnight {
				if d == weekday && s.Start <= hhmm && hhmm < s.End {
					return true
				}
				continue
			}
			if d == weekday && hhmm >= s.Start {
				return true
			}
			if d == prevWeekday && hhmm < s.End {
				return true
			}
		}
	}
	return false
}

// nextScheduleStart は now より後で inSchedule が最初に true になる時刻を返す。
// 7日以内に見つからない場合はゼロ値を返す。
func nextScheduleStart(schedules []Schedule, now time.Time) time.Time {
	t := now.Add(time.Minute).Truncate(time.Minute)
	end := now.Add(7 * 24 * time.Hour)
	for t.Before(end) {
		if inSchedule(schedules, t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func scheduleOutMsg(schedules []Schedule, now time.Time) string {
	next := nextScheduleStart(schedules, now)
	if next.IsZero() {
		return "現在はスケジュール外です。（7日以内に受付可能なスケジュールがありません）"
	}
	return fmt.Sprintf("現在はスケジュール外です。次の受付時間: %s %s〜",
		jaWeekday[int(next.Weekday())], next.Format("15:04"))
}

// ---- ゲートチェック ----

// roundUpMinutes は remaining を切り上げた分数で返す（表示用）。
func roundUpMinutes(remaining time.Duration) int {
	mins := int(remaining.Minutes())
	if remaining%time.Minute > 0 {
		mins++
	}
	return mins
}

// gateResult は evaluateDelayGate が返す判定結果。
type gateResult int

const (
	gateOutOfSchedule   gateResult = iota // スケジュール外（常に却下）
	gateProceedNoDelay                    // 遅延未設定・実行可能
	gateNeedsNewPending                   // 保留なし・新規作成が必要
	gateWaiting                           // 保留あり・タイマー未切れ
	gateExpired                           // 保留あり・タイマー切れ・実行可能
	gateStale                             // 保留あり・実行可能な状態のまま長時間放置され失効・再要求が必要
)

// evaluateDelayGate はスケジュール・遅延に基づく可否判定を行う共通ロジック。
// PendingReq（閲覧・解除要求）と PendingCfg（設定変更要求）の両方から共有される。
//
// B案ルール: スケジュールは常に最優先。スケジュール外なら、タイマー状態によらず常に却下。
//
// タイマーが切れて実行可能になった後も、要求時点の遅延時間（pendingDelaySeconds）と同じ
// 長さの猶予期間内に実行されなければ失効（gateStale）とみなし、再要求を必要とする。
// これにより「実行可能な状態を長時間放置したら再度制限がかかる」を実現する。
func evaluateDelayGate(schedules []Schedule, delaySeconds int, hasPending bool, requestedAt time.Time, pendingDelaySeconds int, now time.Time) (gateResult, time.Duration) {
	if len(schedules) > 0 && !inSchedule(schedules, now) {
		return gateOutOfSchedule, 0
	}
	if delaySeconds == 0 {
		return gateProceedNoDelay, 0
	}
	if !hasPending {
		return gateNeedsNewPending, 0
	}
	remaining := time.Duration(pendingDelaySeconds)*time.Second - now.Sub(requestedAt)
	if remaining > 0 {
		return gateWaiting, remaining
	}
	graceWindow := time.Duration(pendingDelaySeconds) * time.Second
	staleFor := -remaining
	if staleFor > graceWindow {
		return gateStale, 0
	}
	return gateExpired, 0
}

// gateCheck はスケジュール・遅延に基づいて操作の可否を判定する（PendingReq 操作用）。
//
// 戻り値:
//
//	proceed=true   → 操作を実行してよい
//	needsSave=true → Config の状態が変化したため呼び出し元で saveConfig が必要
//	msg            → proceed=false のときユーザーへ表示するメッセージ
func gateCheck(c *Config, pending **PendingReq, now time.Time) (proceed bool, needsSave bool, msg string) {
	var hasPending bool
	var requestedAt time.Time
	var pendingDelay int
	if *pending != nil {
		hasPending = true
		requestedAt = (*pending).RequestedAt
		pendingDelay = (*pending).DelaySeconds
	}

	result, remaining := evaluateDelayGate(c.Schedules, c.DelaySeconds, hasPending, requestedAt, pendingDelay, now)
	switch result {
	case gateOutOfSchedule:
		return false, false, scheduleOutMsg(c.Schedules, now)
	case gateProceedNoDelay:
		return true, false, ""
	case gateNeedsNewPending:
		*pending = &PendingReq{RequestedAt: now, DelaySeconds: c.DelaySeconds}
		t := now.Add(time.Duration(c.DelaySeconds) * time.Second)
		return false, true, fmt.Sprintf("要求を受け付けました。%d分後（%s以降）にスケジュール内で再実行してください。",
			c.DelaySeconds/60, t.Format("15:04"))
	case gateWaiting:
		return false, false, fmt.Sprintf("あと約%d分で実行可能です（%s以降）。",
			roundUpMinutes(remaining), now.Add(remaining).Format("15:04"))
	case gateStale:
		*pending = &PendingReq{RequestedAt: now, DelaySeconds: c.DelaySeconds}
		t := now.Add(time.Duration(c.DelaySeconds) * time.Second)
		return false, true, fmt.Sprintf("実行可能な状態のまま長時間放置されたため要求が失効しました。再度要求を受け付けました。%d分後（%s以降）にスケジュール内で再実行してください。",
			c.DelaySeconds/60, t.Format("15:04"))
	default: // gateExpired
		*pending = nil
		return true, true, ""
	}
}

// ---- config サブコマンド ----

type multiString []string

func (m *multiString) String() string     { return strings.Join(*m, ", ") }
func (m *multiString) Set(v string) error { *m = append(*m, v); return nil }

const configHelpText = `使い方: passcode-gen config [show | set ... | disable | help]

  show      現在の設定と保留状況を表示（引数なしも同じ）
  set       閲覧制限を設定する
  disable   閲覧制限を解除する
  help      このヘルプを表示する

config set のオプション:
  --delay <時間>            遅延時間 (例: 30m, 1h, 2h30m)
  --schedule <スケジュール>  曜日と時刻帯 (繰り返し指定で複数スロット)

スケジュール書式:
  "日 09:00-11:00"       日曜 9〜11時
  "月-金 20:00-22:00"    平日 20〜22時
  "土,日 10:00-12:00"    土日 10〜12時
  ※ 曜日は英語 (Mon-Sun) でも指定可 / レンジ・カンマ混在可

注意:
  - config.bin を削除するとログ (log.bin) も永久に復号できなくなります。
  - 遅延タイマーが切れて実行可能になった後も、遅延時間と同じ長さの猶予期間を
    超えて放置すると要求は失効し、再度要求からやり直しになります
    （log 閲覧・設定変更・制限解除のすべてに適用）。
`

func runConfigCmd(args []string) error {
	if len(args) == 0 {
		return showConfigCmd()
	}
	switch args[0] {
	case "show":
		return showConfigCmd()
	case "set":
		return configSet(args[1:])
	case "disable":
		return configDisable()
	case "help", "-h", "--help":
		fmt.Print(configHelpText)
		return nil
	default:
		return fmt.Errorf("不明なサブコマンド: %q\n\n%s", args[0], configHelpText)
	}
}

func showConfigCmd() error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	if c == nil || (c.DelaySeconds == 0 && len(c.Schedules) == 0) {
		fmt.Println("制限は設定されていません。")
		if c == nil {
			return nil
		}
	} else {
		if c.DelaySeconds > 0 {
			fmt.Printf("遅延: %d分\n", c.DelaySeconds/60)
		}
		if len(c.Schedules) > 0 {
			fmt.Println("スケジュール:")
			for _, s := range c.Schedules {
				names := make([]string, len(s.Days))
				for i, d := range s.Days {
					names[i] = jaWeekday[d]
				}
				fmt.Printf("  %s %s〜%s\n", strings.Join(names, "/"), s.Start, s.End)
			}
		}
	}

	now := time.Now()
	printPendingReq("閲覧要求", c.PendingView, now)
	printPendingCfg(c.PendingConfig, now)
	printPendingReq("解除要求", c.PendingDisable, now)
	return nil
}

// pendingStatusText は残り時間・要求時点の遅延秒数から表示用の状態文字列を返す。
// 実行可能になった後も猶予期間（要求時点の遅延と同じ長さ）を超えて放置されると
// 「失効」とみなす（gateStale と同じ基準）。
func pendingStatusText(remaining time.Duration, pendingDelaySeconds int, now time.Time) string {
	if remaining > 0 {
		return fmt.Sprintf("あと約%d分（%s以降）", roundUpMinutes(remaining), now.Add(remaining).Format("15:04"))
	}
	graceWindow := time.Duration(pendingDelaySeconds) * time.Second
	if -remaining > graceWindow {
		return "失効済み（実行可能な状態のまま長時間放置されたため。次回実行時に再要求されます）"
	}
	return "実行可能（スケジュール内で再実行してください）"
}

func printPendingReq(label string, p *PendingReq, now time.Time) {
	if p == nil {
		fmt.Printf("保留中の%s: なし\n", label)
		return
	}
	remaining := time.Duration(p.DelaySeconds)*time.Second - now.Sub(p.RequestedAt)
	fmt.Printf("保留中の%s: %s\n", label, pendingStatusText(remaining, p.DelaySeconds, now))
}

func printPendingCfg(p *PendingCfg, now time.Time) {
	if p == nil {
		fmt.Println("保留中の設定変更: なし")
		return
	}
	remaining := time.Duration(p.WaitSeconds)*time.Second - now.Sub(p.RequestedAt)
	fmt.Printf("保留中の設定変更: %s\n", pendingStatusText(remaining, p.WaitSeconds, now))
}

// configSet は閲覧制限を設定する。
// 初回（config.bin なし）は即時適用。それ以降は evaluateDelayGate を経由する。
// --delay・--schedule のうち指定されなかった側は既存設定を維持する（片方だけの変更を可能にする）。
func configSet(args []string) error {
	fs := flag.NewFlagSet("config set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var delayStr string
	var scheduleStrs multiString
	fs.StringVar(&delayStr, "delay", "", "遅延時間")
	fs.Var(&scheduleStrs, "schedule", "スケジュール（繰り返し指定可）")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if delayStr == "" && len(scheduleStrs) == 0 {
		return fmt.Errorf("config set には --delay または --schedule が必要です\n\n%s", configHelpText)
	}

	var delayGiven int
	if delayStr != "" {
		d, err := time.ParseDuration(delayStr)
		if err != nil || d <= 0 {
			return fmt.Errorf("遅延時間の書式が不正です: %q (例: 30m, 1h)", delayStr)
		}
		delayGiven = int(d.Seconds())
	}
	var schedulesGiven []Schedule
	for _, s := range scheduleStrs {
		sch, err := parseSchedule(s)
		if err != nil {
			return err
		}
		schedulesGiven = append(schedulesGiven, sch)
	}

	c, err := loadConfig()
	if err != nil {
		return err
	}

	// 初回（config.bin なし）→ 即時適用・新規 salt 生成
	if c == nil {
		salt := make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return err
		}
		c = &Config{Salt: salt, DelaySeconds: delayGiven, Schedules: schedulesGiven}
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Println("設定を適用しました。")
		return nil
	}

	now := time.Now()
	var hasPending bool
	var requestedAt time.Time
	var pendingWait int
	if c.PendingConfig != nil {
		hasPending = true
		requestedAt = c.PendingConfig.RequestedAt
		pendingWait = c.PendingConfig.WaitSeconds
	}

	result, _ := evaluateDelayGate(c.Schedules, c.DelaySeconds, hasPending, requestedAt, pendingWait, now)

	// 指定されなかった側は既存設定を維持する（他方だけを黙って0/nilに戻さない）。
	// gateWaiting（保留がまだ有効）の場合は、現在アクティブな設定ではなく、
	// 既にステージ済みの保留内容を基準にする（そうしないと保留中の未適用の変更が
	// 「後勝ち」上書きで消えてしまう）。
	baseDelay, baseSchedules := c.DelaySeconds, c.Schedules
	if result == gateWaiting {
		baseDelay, baseSchedules = c.PendingConfig.NewDelaySeconds, c.PendingConfig.NewSchedules
	}
	effectiveDelay := baseDelay
	if delayStr != "" {
		effectiveDelay = delayGiven
	}
	effectiveSchedules := baseSchedules
	if len(scheduleStrs) > 0 {
		effectiveSchedules = schedulesGiven
	}
	switch result {
	case gateOutOfSchedule:
		fmt.Println(scheduleOutMsg(c.Schedules, now))
		return nil
	case gateProceedNoDelay:
		c.DelaySeconds = effectiveDelay
		c.Schedules = effectiveSchedules
		c.PendingConfig = nil
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Println("設定を変更しました。")
		return nil
	case gateNeedsNewPending:
		c.PendingConfig = &PendingCfg{
			RequestedAt:     now,
			WaitSeconds:     c.DelaySeconds,
			NewDelaySeconds: effectiveDelay,
			NewSchedules:    effectiveSchedules,
		}
		t := now.Add(time.Duration(c.DelaySeconds) * time.Second)
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Printf("変更を受け付けました。%d分後（%s以降）にスケジュール内で再実行してください。\n",
			c.DelaySeconds/60, t.Format("15:04"))
		return nil
	case gateStale:
		// 実行可能な状態のまま長時間放置され失効 → 新規要求として再作成
		c.PendingConfig = &PendingCfg{
			RequestedAt:     now,
			WaitSeconds:     c.DelaySeconds,
			NewDelaySeconds: effectiveDelay,
			NewSchedules:    effectiveSchedules,
		}
		t := now.Add(time.Duration(c.DelaySeconds) * time.Second)
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Printf("実行可能な状態のまま長時間放置されたため要求が失効しました。再度要求を受け付けました。%d分後（%s以降）にスケジュール内で再実行してください。\n",
			c.DelaySeconds/60, t.Format("15:04"))
		return nil
	case gateWaiting:
		// タイマー待機中 → 後勝ちで上書き・タイマーリセット
		c.PendingConfig.RequestedAt = now
		c.PendingConfig.WaitSeconds = c.DelaySeconds
		c.PendingConfig.NewDelaySeconds = effectiveDelay
		c.PendingConfig.NewSchedules = effectiveSchedules
		t := now.Add(time.Duration(c.DelaySeconds) * time.Second)
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Printf("要求を更新しました。%d分後（%s以降）にスケジュール内で再実行してください。\n",
			c.DelaySeconds/60, t.Format("15:04"))
		return nil
	default: // gateExpired
		c.DelaySeconds = c.PendingConfig.NewDelaySeconds
		c.Schedules = c.PendingConfig.NewSchedules
		c.PendingConfig = nil
		if err := saveConfig(c); err != nil {
			return err
		}
		fmt.Println("設定を変更しました。")
		return nil
	}
}

// configDisable は閲覧制限を解除する。gateCheck を経由する。
func configDisable() error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	if c == nil || (c.DelaySeconds == 0 && len(c.Schedules) == 0) {
		fmt.Println("制限は設定されていません。")
		return nil
	}

	now := time.Now()
	proceed, needsSave, msg := gateCheck(c, &c.PendingDisable, now)
	if needsSave {
		if err := saveConfig(c); err != nil {
			return err
		}
	}
	if !proceed {
		fmt.Println(msg)
		return nil
	}

	c.DelaySeconds = 0
	c.Schedules = nil
	c.PendingView = nil
	c.PendingConfig = nil
	c.PendingDisable = nil
	if err := saveConfig(c); err != nil {
		return err
	}
	fmt.Println("制限を解除しました。")
	return nil
}
