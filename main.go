package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"

	"golang.org/x/term"
)

const (
	defaultDigits = 4  // 桁数を省略したときの既定値
	minDigits     = 4  // 弱パターン判定が成立する下限
	maxDigits     = 10 // 現実的なPIN長の上限

	defaultCount = 1   // 生成個数を省略したときの既定値
	minCount     = 1   // 生成個数の下限
	maxCount     = 100 // 生成個数の上限（最小候補数 166（4桁）未満なので重複排除が常に成立）
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

// isCorner はキー b がキーパッドの角 {1,3,7,9} かどうかを返す。
func isCorner(b byte) bool {
	return b == '1' || b == '3' || b == '7' || b == '9'
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
	for i := 1; i < len(pw); i++ {
		a, b := pw[i-1], pw[i]
		if isCorner(a) && isCorner(b) && a != b {
			return true
		}
		if (a == '2' && b == '0') || (a == '0' && b == '2') {
			return true
		}
	}

	return false
}

// sentinel は「直前の桁が存在しない」ことを表す番兵。'0'..'9'(48..57) と衝突しない。
const sentinel byte = 0

// idx は桁バイトをテーブル添字へ変換する（sentinel→10、'0'..'9'→0..9）。
func idx(b byte) int {
	if b == sentinel {
		return 10
	}
	return int(b - '0')
}

// unidx は idx の逆変換（10→sentinel、0..9→'0'..'9'）。
func unidx(i int) byte {
	if i == 10 {
		return sentinel
	}
	return byte('0' + i)
}

// canFollow は直前2桁 (prev2, prev1) の後ろに cand を置けるかを返す。
// isWeak の「連番以外」の局所ルールと厳密に一致させている。
// prev1==sentinel は1桁目（無条件許可）、prev2==sentinel は飛ばし制約なしを表す。
func canFollow(prev2, prev1, cand byte) bool {
	if prev1 != sentinel {
		if cand == prev1 { // 連続同一桁
			return false
		}
		if adjacent(prev1, cand) { // キーパッド隣接
			return false
		}
		if isCorner(prev1) && isCorner(cand) { // 角→角（cand!=prev1 は上で担保済み）
			return false
		}
		if (prev1 == '2' && cand == '0') || (prev1 == '0' && cand == '2') { // 2↔0
			return false
		}
	}
	if prev2 != sentinel && cand == prev2 { // 飛ばし繰り返し（i桁目==i+2桁目）
		return false
	}
	return true
}

// buildTable は後ろ向き DP で並び数を数える。
// ways[r][idx(p2)][idx(p1)] = 直前2桁が (p2,p1) のとき、局所制約を満たして
// 残り r 桁を埋められる並びの総数。
func buildTable(digits int) [][11][11]int {
	ways := make([][11][11]int, digits+1)
	for a := 0; a < 11; a++ {
		for b := 0; b < 11; b++ {
			ways[0][a][b] = 1
		}
	}
	for r := 1; r <= digits; r++ {
		for a := 0; a < 11; a++ {
			for b := 0; b < 11; b++ {
				p2, p1 := unidx(a), unidx(b)
				sum := 0
				for d := 0; d < 10; d++ {
					cand := byte('0' + d)
					if canFollow(p2, p1, cand) {
						sum += ways[r-1][b][idx(cand)]
					}
				}
				ways[r][a][b] = sum
			}
		}
	}
	return ways
}

// samplePIN は buildTable のテーブルを用い、局所制約を満たす digits 桁の並びを
// 「その桁を選んだ先に存在する並び数」で重み付けして一様に1個サンプリングする。
// 重み>0 の候補のみ選ぶため、行き止まりは原理的に発生しない。
func samplePIN(ways [][11][11]int, digits int) ([]byte, error) {
	pw := make([]byte, 0, digits)
	prev2, prev1 := sentinel, sentinel
	for r := digits; r >= 1; r-- {
		var cands, weights [10]int
		m, total := 0, 0
		for d := 0; d < 10; d++ {
			cand := byte('0' + d)
			if !canFollow(prev2, prev1, cand) {
				continue
			}
			w := ways[r-1][idx(prev1)][idx(cand)] // この桁を置いた先の並び数
			if w == 0 {
				continue
			}
			cands[m], weights[m] = d, w
			total += w
			m++
		}
		nbig, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
		if err != nil {
			return nil, err
		}
		x := int(nbig.Int64())
		chosen := cands[m-1]
		for j := 0; j < m; j++ {
			if x < weights[j] {
				chosen = cands[j]
				break
			}
			x -= weights[j]
		}
		c := byte('0' + chosen)
		pw = append(pw, c)
		prev2, prev1 = prev1, c
	}
	return pw, nil
}

// parseFlags はコマンドライン引数を解析して桁数・生成個数・ステップモードを返す。
// -d / --digits で桁数（省略時 defaultDigits）、-n / --number で個数（省略時 defaultCount）、
// -s / --step でステップ表示モードを指定できる。ステップモード時は count を 1 に強制する。
// 範囲外・余分な引数・解析不能ならエラーを返す。-h / --help は usage を
// 標準出力に表示して flag.ErrHelp を返す。
func parseFlags(args []string) (digits, count int, stepMode bool, err error) {
	fs := flag.NewFlagSet("passcode-gen", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 解析エラー時の自動 usage を抑制（help 時はこの後 stdout に出し直す）
	fs.IntVar(&digits, "digits", defaultDigits, fmt.Sprintf("生成する桁数 (%d〜%d)", minDigits, maxDigits))
	fs.IntVar(&digits, "d", defaultDigits, fmt.Sprintf("生成する桁数 (%d〜%d) の短縮形", minDigits, maxDigits))
	fs.IntVar(&count, "number", defaultCount, fmt.Sprintf("生成する個数 (%d〜%d)", minCount, maxCount))
	fs.IntVar(&count, "n", defaultCount, fmt.Sprintf("生成する個数 (%d〜%d) の短縮形", minCount, maxCount))
	fs.BoolVar(&stepMode, "step", false, "1桁ずつ表示するステップモードで実行する")
	fs.BoolVar(&stepMode, "s", false, "ステップモードの短縮形")
	if perr := fs.Parse(args); perr != nil {
		if errors.Is(perr, flag.ErrHelp) {
			// -h/--help は成功扱い。usage を標準出力へ出して ErrHelp を伝播する。
			fs.SetOutput(os.Stdout)
			fs.Usage()
		}
		return 0, 0, false, perr
	}
	if fs.NArg() > 0 {
		return 0, 0, false, fmt.Errorf("余分な引数があります: %v（桁数は -d / --digits、個数は -n / --number で指定してください）", fs.Args())
	}
	if digits < minDigits || digits > maxDigits {
		return 0, 0, false, fmt.Errorf("桁数は %d〜%d で指定してください: %d", minDigits, maxDigits, digits)
	}
	if count < minCount || count > maxCount {
		return 0, 0, false, fmt.Errorf("生成数は %d〜%d で指定してください: %d", minCount, maxCount, count)
	}
	if stepMode {
		count = 1
	}
	return digits, count, stepMode, nil
}

// runStepMode は PIN を1桁ずつ表示するインタラクティブモードを実行する。
// Enter で次桁へ進み（最終桁の後は先頭に戻る）、ESC または Ctrl+C で終了する。
func runStepMode(pin string) error {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return fmt.Errorf("ターミナルを開けません: %w", err)
	}
	defer tty.Close()

	fmt.Println("[Enter] 次の桁  [ESC / Ctrl+C] 終了")

	fd := int(tty.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("ターミナルをrawモードにできません: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	n := len(pin)
	i := 0
	fmt.Printf("%c  (%d/%d)", pin[i], i+1, n)

	buf := make([]byte, 1)
	for {
		if _, err := tty.Read(buf); err != nil {
			return err
		}
		switch buf[0] {
		case 0x1b, 0x03: // ESC または Ctrl+C
			fmt.Print("\r\033[K")
			return nil
		case 0x0d: // Enter (rawモードでは CR)
			i = (i + 1) % n
			fmt.Printf("\r%c  (%d/%d)\033[K", pin[i], i+1, n)
		}
	}
}

// generatePINs は弱パターンを除外しつつ、互いに重複しない count 個の PIN を生成する。
// 構築法（局所制約を満たす並びを重み付き一様サンプリング）で生成し、大域制約の連番だけは
// 最終 isWeak チェックで除外する（連番は稀なので棄却コストは無視できる）。
func generatePINs(digits, count int) ([]string, error) {
	ways := buildTable(digits)
	seen := make(map[string]bool, count)
	pins := make([]string, 0, count)
	for len(pins) < count {
		pw, err := samplePIN(ways, digits)
		if err != nil {
			return nil, err
		}
		s := string(pw)
		if isWeak(pw) || seen[s] { // 連番の除外＋局所実装の保険、および重複排除
			continue
		}
		seen[s] = true
		pins = append(pins, s)
	}
	return pins, nil
}

func main() {
	digits, count, stepMode, err := parseFlags(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0) // usage は parseFlags が標準出力に表示済み
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	pins, err := generatePINs(digits, count)
	if err != nil {
		fmt.Fprintln(os.Stderr, "パスワード生成に失敗しました:", err)
		os.Exit(1)
	}
	if stepMode {
		if err := runStepMode(pins[0]); err != nil {
			fmt.Fprintln(os.Stderr, "ステップ表示中にエラーが発生しました:", err)
			os.Exit(1)
		}
		fmt.Println()
		return
	}
	for _, p := range pins {
		fmt.Println(p)
	}
}
