package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
)

const digits = 4

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
func isWeak(pw []byte) bool {
	// 連続する同一桁（ゾロ目を含む）
	for i := 1; i < digits; i++ {
		if pw[i] == pw[i-1] {
			return true
		}
	}

	// 連番（昇順・降順）
	asc, desc := true, true
	for i := 1; i < digits; i++ {
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

	// 1桁飛ばしの繰り返し（1桁目==3桁目、または2桁目==4桁目）
	// ABAB はこれに包含される。AABB は連続同一桁ルールで除外済み。
	if pw[0] == pw[2] || pw[1] == pw[3] {
		return true
	}

	// キーパッド隣接（斜め含む・一部でも該当で除外）
	for i := 1; i < digits; i++ {
		if adjacent(pw[i-1], pw[i]) {
			return true
		}
	}

	// 角→角（連続する2桁がともに角 {1,3,7,9} で異なる）／ 2↔0
	corners := map[byte]bool{'1': true, '3': true, '7': true, '9': true}
	for i := 1; i < digits; i++ {
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

// generate は crypto/rand を用いて 4 桁の数字文字列を生成する。
func generate() ([]byte, error) {
	pw := make([]byte, digits)
	for i := 0; i < digits; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return nil, err
		}
		pw[i] = byte('0' + n.Int64())
	}
	return pw, nil
}

func main() {
	for {
		pw, err := generate()
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
