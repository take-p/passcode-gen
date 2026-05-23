package main

import (
	"fmt"
	"testing"
)

// 各除外ルールに該当する「弱い」暗証番号は isWeak が true を返すこと。
func TestIsWeak_除外される(t *testing.T) {
	cases := map[string][]string{
		"連続同一桁":  {"6000", "8110", "2922", "0000", "1111", "9999"},
		"連番":     {"1234", "4321", "0123", "3210", "6789", "9876"},
		"飛ばし繰り返し": {"1212", "9494", "8181", "0103", "0161", "8343"},
		"キーパッド隣接": {"1236", "1478", "1402", "9061"},
		"角→角":    {"1300", "3100", "1700", "1900", "3700", "7900"},
		"2と0":    {"2099", "8023", "2055", "0255"},
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
		for i := 1; i < digits; i++ {
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

// generate は常に 4 桁の数字列を返すこと（弱さの除外は main 側のループが担う）。
func TestGenerate(t *testing.T) {
	for i := 0; i < 1000; i++ {
		pw, err := generate()
		if err != nil {
			t.Fatalf("generate がエラー: %v", err)
		}
		if len(pw) != digits {
			t.Fatalf("桁数が %d（期待 %d）: %s", len(pw), digits, pw)
		}
	}
}
