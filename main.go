package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
)

const (
	defaultDigits = 4  // 桁数を省略したときの既定値
	minDigits     = 4  // 弱パターン判定が成立する下限
	maxDigits     = 10 // 現実的なPIN長の上限
)

// keypad は各数字キーの (行, 列) 座標。iPhone のテンキー配置を表す。
//
//	1 2 3
//	4 5 6
//	7 8 9
//	  0
var keypad = map[byte][2]int{
	'1': {0, 0}, '2': {0, 1}, '3': {0, 2},
	'4': {1, 0}, '5': {1, 1}, '6': {1, 2},
	'7': {2, 0}, '8': {2, 1}, '9': {2, 2},
	'0': {3, 1},
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// adjacent は 2 つの数字キーがキーパッド上で隣接しているかを判定する。
// 上下左右に加え斜めも隣接とみなす（チェスのキングの動き）。
func adjacent(a, b byte) bool {
	pa, pb := keypad[a], keypad[b]
	dr, dc := abs(pa[0]-pb[0]), abs(pa[1]-pb[1])
	return dr <= 1 && dc <= 1 && !(dr == 0 && dc == 0)
}

// isWeak は推測されやすい「弱い」暗証番号かどうかを判定する。
// 桁数は pw の長さで決まり、4 桁以上の任意の長さを受け付ける。
func isWeak(pw []byte) bool {
	// 連続する同一桁（ゾロ目を含む）
	for i := 1; i < len(pw); i++ {
		if pw[i] == pw[i-1] {
			return true
		}
	}

	// 連番（昇順・降順）
	asc, desc := true, true
	for i := 1; i < len(pw); i++ {
		if pw[i]-pw[i-1] != 1 {
			asc = false
		}
		if pw[i-1]-pw[i] != 1 {
			desc = false
		}
	}
	if asc || desc {
		return true
	}

	// 1桁飛ばしの繰り返し（i桁目とi+2桁目が同じ）
	// ABAB はこれに包含される。AABB は連続同一桁ルールで除外済み。
	for i := 0; i+2 < len(pw); i++ {
		if pw[i] == pw[i+2] {
			return true
		}
	}

	// キーパッド隣接（斜め含む・一部でも該当で除外）
	for i := 1; i < len(pw); i++ {
		if adjacent(pw[i-1], pw[i]) {
			return true
		}
	}

	// 角→角（連続する2桁がともに角 {1,3,7,9} で異なる）／ 2↔0
	corners := map[byte]bool{'1': true, '3': true, '7': true, '9': true}
	for i := 1; i < len(pw); i++ {
		a, b := pw[i-1], pw[i]
		if corners[a] && corners[b] && a != b {
			return true
		}
		if (a == '2' && b == '0') || (a == '0' && b == '2') {
			return true
		}
	}

	return false
}

// generate は crypto/rand を用いて n 桁の数字文字列を生成する。
func generate(n int) ([]byte, error) {
	pw := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return nil, err
		}
		pw[i] = byte('0' + v.Int64())
	}
	return pw, nil
}

// parseDigits はコマンドライン引数を解析して桁数を返す。
// -d / --digits で指定でき、省略時は defaultDigits。
// minDigits〜maxDigits の範囲外、または解析不能ならエラーを返す。
func parseDigits(args []string) (int, error) {
	fs := flag.NewFlagSet("passcode-gen", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // usage 出力は抑制し、メッセージは呼び出し側に一本化する
	var n int
	fs.IntVar(&n, "digits", defaultDigits, fmt.Sprintf("生成する桁数 (%d〜%d)", minDigits, maxDigits))
	fs.IntVar(&n, "d", defaultDigits, fmt.Sprintf("生成する桁数 (%d〜%d) の短縮形", minDigits, maxDigits))
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// -h/--help は成功扱い。usage を標準出力へ出して ErrHelp を伝播する。
			fs.SetOutput(os.Stdout)
			fs.Usage()
		}
		return 0, err
	}
	if fs.NArg() > 0 {
		return 0, fmt.Errorf("余分な引数があります: %v（桁数は -d / --digits で指定してください）", fs.Args())
	}
	if n < minDigits || n > maxDigits {
		return 0, fmt.Errorf("桁数は %d〜%d で指定してください: %d", minDigits, maxDigits, n)
	}
	return n, nil
}

func main() {
	n, err := parseDigits(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0) // usage は parseDigits が標準出力に表示済み
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for {
		pw, err := generate(n)
		if err != nil {
			fmt.Fprintln(os.Stderr, "パスワード生成に失敗しました:", err)
			os.Exit(1)
		}
		if !isWeak(pw) {
			fmt.Println(string(pw))
			return
		}
	}
}
