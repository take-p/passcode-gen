package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"testing"
)

// 各除外ルールに該当する「弱い」暗証番号は isWeak が true を返すこと。
func TestIsWeak_除外される(t *testing.T) {
	cases := map[string][]string{
		"連続同一桁":   {"6000", "8110", "2922", "0000", "1111", "9999"},
		"連番":      {"1234", "4321", "0123", "3210", "6789", "9876"},
		"飛ばし繰り返し": {"1212", "9494", "8181", "0103", "0161", "8343"},
		"キーパッド隣接": {"1236", "1478", "1402", "9061"},
		"角→角":     {"1300", "3100", "1700", "1900", "3700", "7900"},
		"2と0":     {"2099", "8023", "2055", "0255"},
	}
	for rule, list := range cases {
		for _, s := range list {
			if !isWeak([]byte(s)) {
				t.Errorf("[%s] %s は除外されるべきだが許可された", rule, s)
			}
		}
	}
}

// 全ルールを満たす暗証番号は isWeak が false を返すこと。
func TestIsWeak_許可される(t *testing.T) {
	for _, s := range []string{"6018", "7605", "6401", "0461", "0164", "6103"} {
		if isWeak([]byte(s)) {
			t.Errorf("%s は有効なはずだが除外された", s)
		}
	}
}

// adjacent はキーパッド上で上下左右・斜めに隣接するキーのみ true を返すこと。
func TestAdjacent(t *testing.T) {
	adj := [][2]byte{{'1', '2'}, {'1', '4'}, {'1', '5'}, {'5', '9'}, {'8', '0'}, {'7', '0'}, {'9', '0'}}
	for _, p := range adj {
		if !adjacent(p[0], p[1]) {
			t.Errorf("%c と %c は隣接のはず", p[0], p[1])
		}
	}
	notAdj := [][2]byte{{'1', '3'}, {'1', '9'}, {'1', '6'}, {'4', '0'}, {'1', '0'}}
	for _, p := range notAdj {
		if adjacent(p[0], p[1]) {
			t.Errorf("%c と %c は非隣接のはず", p[0], p[1])
		}
	}
}

// 全 10000 通りを走査し、有効な暗証番号が想定数で、弱いものが残らないこと。
func TestFullScan(t *testing.T) {
	const wantValid = 166
	valid := 0
	for n := 0; n < 10000; n++ {
		s := fmt.Sprintf("%04d", n)
		if isWeak([]byte(s)) {
			continue
		}
		valid++
		// 有効と判定されたものに弱いパターンが紛れていないか二重チェック
		for i := 1; i < defaultDigits; i++ {
			if s[i] == s[i-1] {
				t.Errorf("有効判定の %s に連続同一桁が残っている", s)
			}
		}
		if s[0] == s[2] || s[1] == s[3] {
			t.Errorf("有効判定の %s に飛ばし繰り返しが残っている", s)
		}
	}
	if valid != wantValid {
		t.Errorf("有効な暗証番号の数 = %d, 期待値 = %d", valid, wantValid)
	}
}

// generate は指定した桁数の数字列を返すこと（弱さの除外は main 側のループが担う）。
func TestGenerate(t *testing.T) {
	for _, n := range []int{minDigits, defaultDigits, 6, maxDigits} {
		for i := 0; i < 500; i++ {
			pw, err := generate(n)
			if err != nil {
				t.Fatalf("generate(%d) がエラー: %v", n, err)
			}
			if len(pw) != n {
				t.Fatalf("桁数が %d（期待 %d）: %s", len(pw), n, pw)
			}
			for _, c := range pw {
				if c < '0' || c > '9' {
					t.Fatalf("数字以外が含まれる: %s", pw)
				}
			}
		}
	}
}

// isWeak は 4 桁より長い入力でも panic せず、桁数に応じた一般化ルールが働くこと。
func TestIsWeak_可変桁(t *testing.T) {
	// 飛ばし繰り返し（i桁目==i+2桁目）の一般化を isolate して検証する。
	// 前提: 先頭4桁 "0160" は単体では有効（どのルールにも該当しない）。
	if isWeak([]byte("0160")) {
		t.Fatal(`前提崩れ: 4桁 "0160" は有効であるべき（飛ばし繰り返しの一般化を isolate できない）`)
	}
	// "016010" が弱いのは pw[3]==pw[5]（i=3）のみ。4桁固定ロジック
	// （pw[0]==pw[2] || pw[1]==pw[3]）では捕捉できず、一般化で初めて検出される。
	// "010101" は i=0 で該当する従来相当ケース。
	for _, s := range []string{"016010", "010101"} {
		if !isWeak([]byte(s)) {
			t.Errorf("%s（%d桁）は飛ばし繰り返しで除外されるべき", s, len(s))
		}
	}

	// 飛ばし繰り返し以外のルールも可変長で panic せず働くこと。
	others := map[string][]string{
		"連続同一桁":   {"600000", "8110000000"},
		"連番":      {"123456", "0123456789"},
		"キーパッド隣接": {"741203", "1478520369"},
	}
	for rule, list := range others {
		for _, s := range list {
			if !isWeak([]byte(s)) {
				t.Errorf("[%s] %s（%d桁）は除外されるべき", rule, s, len(s))
			}
		}
	}

	// 各桁数で有効な候補が少なくとも1つ存在すること（無限ループ防止の担保）。
	for _, n := range []int{minDigits, 6, 8, maxDigits} {
		if !hasValidPIN(n) {
			t.Errorf("%d桁で有効な暗証番号が1つも見つからない", n)
		}
	}
}

// hasValidPIN は n 桁で isWeak を通過する数字列が存在するかを、
// 決定的なシードのランダム探索で確かめる（有効な候補は早期に見つかる）。
func hasValidPIN(n int) bool {
	rng := rand.New(rand.NewSource(1))
	pw := make([]byte, n)
	for try := 0; try < 1_000_000; try++ {
		for i := range pw {
			pw[i] = byte('0' + rng.Intn(10))
		}
		if !isWeak(pw) {
			return true
		}
	}
	return false
}

// parseFlags は -d/--digits と -n/--number を解釈し、省略時は既定値、範囲外はエラー。
func TestParseFlags(t *testing.T) {
	ok := []struct {
		args       []string
		wantDigits int
		wantCount  int
	}{
		{nil, defaultDigits, defaultCount},
		{[]string{"-d", "6"}, 6, defaultCount},
		{[]string{"--digits", "8"}, 8, defaultCount},
		{[]string{"-digits", "10"}, 10, defaultCount},
		{[]string{"-n", "5"}, defaultDigits, 5},
		{[]string{"--number", "10"}, defaultDigits, 10},
		{[]string{"-d", "8", "-n", "3"}, 8, 3},
		{[]string{"-d", "4", "-n", "1"}, 4, 1},
	}
	for _, c := range ok {
		d, n, err := parseFlags(c.args)
		if err != nil {
			t.Errorf("parseFlags(%v) が予期せぬエラー: %v", c.args, err)
			continue
		}
		if d != c.wantDigits || n != c.wantCount {
			t.Errorf("parseFlags(%v) = (桁数%d, 個数%d), 期待 (桁数%d, 個数%d)", c.args, d, n, c.wantDigits, c.wantCount)
		}
	}

	ng := [][]string{
		{"-d", "3"},          // 桁数 下限未満
		{"-d", "11"},         // 桁数 上限超過
		{"-d", "abc"},        // 非数値
		{"-n", "0"},          // 個数 下限未満
		{"-n", "11"},         // 個数 上限超過
		{"-n", "abc"},        // 非数値
		{"6"},                // 位置引数は受け付けない（黙って無視させない）
		{"6", "-d", "8"},     // 位置引数が先頭にあると後続フラグも解析されない
		{"-d", "8", "extra"}, // 余分な位置引数
	}
	for _, args := range ng {
		if _, _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) はエラーになるべき", args)
		}
	}
}

// -h / --help は flag.ErrHelp を返し、usage を標準出力へ出すこと（エラー終了ではない）。
func TestParseFlags_ヘルプ(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			orig := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe: %v", err)
			}
			defer func() { os.Stdout = orig }() // panic 時も含め確実に復元する
			os.Stdout = w

			_, _, perr := parseFlags([]string{arg})
			w.Close()
			out, _ := io.ReadAll(r)

			if !errors.Is(perr, flag.ErrHelp) {
				t.Errorf("parseFlags([%q]) は flag.ErrHelp を返すべき: %v", arg, perr)
			}
			for _, want := range []string{"-digits", "-number"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("usage に %s の説明が含まれるべき: %q", want, out)
				}
			}
		})
	}
}

// generatePINs は count 個・桁数一致・非弱・互いに重複しない PIN を返すこと。
func TestGeneratePINs(t *testing.T) {
	cases := []struct{ digits, count int }{
		{defaultDigits, defaultCount}, // 既定（4桁1個）
		{4, maxCount},                 // 4桁10個（166候補からの重複排除が効く）
		{6, 5},
		{maxDigits, 3},
	}
	for _, c := range cases {
		pins, err := generatePINs(c.digits, c.count)
		if err != nil {
			t.Errorf("generatePINs(%d,%d) がエラー: %v", c.digits, c.count, err)
			continue
		}
		if len(pins) != c.count {
			t.Errorf("generatePINs(%d,%d) の件数 = %d, 期待 %d", c.digits, c.count, len(pins), c.count)
		}
		seen := make(map[string]bool, len(pins))
		for _, p := range pins {
			if len(p) != c.digits {
				t.Errorf("生成 %q の桁数 = %d, 期待 %d", p, len(p), c.digits)
			}
			if isWeak([]byte(p)) {
				t.Errorf("生成 %q が弱判定される", p)
			}
			if seen[p] {
				t.Errorf("生成 %q が重複している", p)
			}
			seen[p] = true
		}
	}
}
