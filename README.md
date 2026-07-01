# passcode-gen

[![CI](https://github.com/take-p/passcode-gen/actions/workflows/ci.yml/badge.svg)](https://github.com/take-p/passcode-gen/actions/workflows/ci.yml)

**English** | [日本語](#japanese)

`passcode-gen` is a CLI tool that generates **PINs** that are hard to guess yet avoid the
obvious, easy-to-remember patterns people tend to pick. It defaults to 4 digits (handy for
things like the iPhone Screen Time passcode), and you can choose 4–10 digits with
`-d` / `--digits`.

## Features

- Uses `crypto/rand` (a cryptographically secure random source)
- Excludes patterns that people tend to choose or that are easy to guess
- Configurable length from 4 to 10 digits (default 4)
- Step mode (`-s`): reveals one digit at a time so you can memorize without exposing the full PIN
- Encrypted log: every generated PIN is saved to `~/.passcode-gen/log.bin` (AES-256-GCM, binary-hash-bound key); readable only with the same binary
- Viewing restriction (`passcode-gen config`): require a delay and/or a schedule window before the log can be viewed

## Installation

With a Go toolchain installed:

```sh
go install github.com/take-p/passcode-gen@latest
```

The `passcode-gen` binary is placed in `$(go env GOPATH)/bin`. Add that directory to your
`PATH` to run `passcode-gen` from anywhere.

## Usage

Each run prints PINs that satisfy the rules. Length defaults to 4 digits and count defaults to 1.

```sh
passcode-gen                # one 4-digit PIN (default)
passcode-gen -d 6           # one 6-digit PIN
passcode-gen --digits 8     # one 8-digit PIN
passcode-gen -n 5           # five 4-digit PINs (all distinct)
passcode-gen -d 6 -n 3      # three 6-digit PINs
passcode-gen -s             # step mode: reveal one digit at a time
passcode-gen -s -d 6        # step mode for a 6-digit PIN
passcode-gen log            # show the encrypted log of past PINs
passcode-gen config         # show current restriction settings
passcode-gen config set --delay 30m --schedule "日 09:00-11:00"
passcode-gen config disable # remove all restrictions
passcode-gen help           # show usage (including subcommands)
passcode-gen -h             # show usage (option list)
```

Length is set with `-d` / `--digits` (**4–10 digits**) and count with `-n` / `--number`
(**1–100**). When you request multiple PINs, all of them are distinct (no duplicates).
Length and count must be passed through these flags — a positional argument such as
`passcode-gen 6` is rejected. Out-of-range values, non-numeric values, or extra arguments
print an error and exit with code 2. `-s` / `--step` and `-n` / `--number` cannot be used
together (step mode always generates exactly one PIN).

> The weak-pattern rules assume at least 4 digits, so 4 is the minimum length.

### Step mode (`-s` / `--step`)

Step mode reveals the PIN one digit at a time — useful when you want to memorize it
without leaving the full number visible on screen.

```
[Enter] 次の桁  [ESC / Ctrl+C] 終了
5  (1/4)
```

- **Enter** — advance to the next digit (wraps back to the first after the last)
- **ESC** or **Ctrl+C** — exit

### Log (`passcode-gen log`)

Every generated PIN is automatically saved to `~/.passcode-gen/log.bin` using
AES-256-GCM encryption. The encryption key is derived from the binary's own SHA-256
hash, so the log can only be decrypted by the same binary that wrote it. Updating the
binary (e.g., `go install`) will make previous log entries unreadable.

```sh
passcode-gen log   # display past PINs, newest first
```

Up to 10 entries are shown at once. If there are more than 10, use the arrow keys (↑/↓)
to scroll. Press **q**, **ESC**, or **Ctrl+C** to exit the viewer.

### Viewing restrictions (`passcode-gen config`)

You can require a **delay** (N minutes between request and viewing) and/or a **schedule**
(specific days/hours when any restricted action is allowed).

```sh
# Set a 30-minute delay, allowed only on Sundays 09:00–11:00
passcode-gen config set --delay 30m --schedule "日 09:00-11:00"

# Multiple schedule slots
passcode-gen config set --delay 30m \
  --schedule "月-金 20:00-22:00" \
  --schedule "土,日 09:00-11:00"

# Show current settings and any pending requests
passcode-gen config

# Remove all restrictions
passcode-gen config disable
```

**Schedule format** — `"<days> <start>-<end>"`:

| Example | Meaning |
|---|---|
| `"日 09:00-11:00"` | Sundays 09:00–11:00 |
| `"月-金 20:00-22:00"` | Mon–Fri 20:00–22:00 |
| `"土,日 10:00-12:00"` | Sat & Sun 10:00–12:00 |

Day names accept both English (`Mon`–`Sun`) and Japanese (`月`–`日`); ranges (`月-金`) and
comma lists (`土,日`) can be combined.

**How the gate works:**

1. If a schedule is set and you're outside it → **rejected** (try again in the next window)
2. If a delay is set:
   - First run → request is recorded; re-run after the delay expires (and while in-schedule)
   - Timer still running → shows remaining time
   - Timer expired → action proceeds; pending is cleared
   - Timer expired but left unattended for longer than the delay itself → the request **goes
     stale** and a new request is recorded instead (applies to log viewing, `config set`, and
     `config disable` alike)
3. No delay, schedule only → allowed immediately whenever you're in-schedule

> **Warning:** `~/.passcode-gen/config.bin` stores the salt used to encrypt `log.bin`.
> Deleting `config.bin` permanently destroys access to the log.

To run or build from a cloned repository:

```sh
go run .                 # run in place
go build -o passcode-gen # build a binary
./passcode-gen
```

## Short alias

`passcode-gen` is a long name, so a shell alias lets you run it with fewer keystrokes. Add the
following line to `~/.zshrc` (or `~/.bashrc` for bash):

```sh
alias pscd-gen='passcode-gen'
```

After reloading your shell config you can run it as `pscd-gen`:

```sh
source ~/.zshrc   # reload (or just open a new shell)
pscd-gen
```

## Excluded patterns

A number that matches any of the rules below is considered "weak" and is regenerated until it
no longer matches.

| Rule | Description | Examples |
| --- | --- | --- |
| Consecutive identical digits | Neighboring digits are the same (includes all-same) | `6000`, `8110`, `0000` |
| Sequential | Ascending or descending run | `1234`, `4321`, `0123` |
| Every-other repeat | The i-th and (i+2)-th digits match (same as two places back) | `1212`, `9494`, `0161`, `016010` |
| Keypad-adjacent | Digits next to each other on the keypad (including diagonals) | `1236`, `1478` |
| Corner-to-corner | Two consecutive digits are both corner keys `1/3/7/9` (incl. diagonal) | `1300`, `1700`, `3700` |
| 2↔0 | Two consecutive digits are `2` and `0` (either order) | `2099`, `8023` |

### Keypad layout

Adjacency is based on the iPhone numeric keypad layout.

```
1 2 3
4 5 6
7 8 9
  0
```

- Adjacent = up/down/left/right plus diagonals (a chess king's move)
- `0` is treated as directly below `8`, so it is adjacent to `8` (directly above) and to
  `7` / `9` (diagonally)

## Number of valid PINs (4 digits)

For 4 digits, out of all 10000 combinations (0000–9999), **166** satisfy every rule above.
Increasing the length raises the absolute number of valid combinations, but the proportion
(hit rate) drops (for reference: about 1.7% at 4 digits, about 0.0005% at 10 digits). The
rules being public narrows the candidate space, but devices and services that use PINs
normally enforce **attempt limits**, so brute force is not a practical concern.

## Testing

```sh
go test ./...
```

This checks each exclusion rule and verifies the valid count by exhaustive enumeration
(166 for 4 digits).

## License

Released under the [MIT License](LICENSE).

---

<a id="japanese"></a>

[English](#passcode-gen) | **日本語**

覚えにくく推測されにくい **暗証番号（PIN）** を生成する CLI アプリです。
デフォルトは 4 桁で、iPhone のペアレンタルコントロールなどを想定しています。
`-d` / `--digits` で 4〜10 桁の範囲で桁数を指定することもできます。

## 特徴

- 乱数源に `crypto/rand`（暗号学的に安全な乱数）を使用
- 人が選びがち・覚えやすく推測されやすいパターンを除外して生成
- 桁数を 4〜10 桁で指定可能（省略時は 4 桁）
- ステップモード（`-s`）: 1 桁ずつ表示し、画面に全桁を晒さず暗記できる
- 暗号化ログ: 生成した PIN を `~/.passcode-gen/log.bin` に AES-256-GCM で保存。鍵はバイナリのハッシュから導出するため、同じバイナリ以外では復号できない
- 閲覧制限（`passcode-gen config`）: ログを表示するまでの遅延時間や、操作可能な曜日・時刻帯を設定できる

## インストール

Go 環境があれば、次のコマンドで導入できます。

```sh
go install github.com/take-p/passcode-gen@latest
```

実行ファイル `passcode-gen` が `$(go env GOPATH)/bin` に置かれます。
そのディレクトリに PATH を通せば、どこからでも `passcode-gen` で実行できます。

## 使い方

実行するたびに、条件を満たす暗証番号を表示します。桁数を省略すると 4 桁、個数を省略すると 1 個です。

```sh
passcode-gen                # 4桁を1個（デフォルト）
passcode-gen -d 6           # 6桁を1個
passcode-gen --digits 8     # 8桁を1個
passcode-gen -n 5           # 4桁を5個（すべて異なる）
passcode-gen -d 6 -n 3      # 6桁を3個
passcode-gen -s             # ステップモード（1桁ずつ表示）
passcode-gen -s -d 6        # 6桁をステップモードで表示
passcode-gen log            # 過去に生成したパスコードのログを表示
passcode-gen config         # 現在の閲覧制限設定を表示
passcode-gen config set --delay 30m --schedule "日 09:00-11:00"
passcode-gen config disable # 制限を解除する
passcode-gen help           # 使い方（サブコマンド含む）を表示
passcode-gen -h             # 使い方（オプション一覧）を表示
```

桁数は `-d` / `--digits` で **4〜10 桁**、個数は `-n` / `--number` で **1〜100 個**を指定できます。
複数個を指定した場合、出力される暗証番号はすべて異なる値になります（重複なし）。
桁数・個数は必ずこれらのフラグで渡してください（`passcode-gen 6` のように位置引数で渡すことはできず、エラーになります）。
範囲外・数値以外・余分な引数を指定した場合はエラーを表示して終了します（終了コード 2）。
`-s` / `--step` と `-n` / `--number` は同時に指定できません（ステップモードは常に 1 個生成）。

> 弱いパターンの除外ルールは 4 桁以上を前提に設計しているため、下限を 4 桁としています。

### ステップモード（`-s` / `--step`）

1 桁ずつ表示するモードです。画面に全桁を晒さず暗記したいときに使えます。

```
[Enter] 次の桁  [ESC / Ctrl+C] 終了
5  (1/4)
```

- **Enter** — 次の桁へ進む（最終桁の後は先頭に戻る）
- **ESC** または **Ctrl+C** — 終了

### ログ（`passcode-gen log`）

生成したパスコードはすべて `~/.passcode-gen/log.bin` に AES-256-GCM で暗号化して自動保存されます。
鍵はバイナリ自身の SHA-256 ハッシュから導出されるため、同じバイナリ以外では復号できません。
`go install` でバイナリを更新すると、それ以前のログは読めなくなります。

```sh
passcode-gen log   # 過去のパスコードを新しい順に表示
```

一度に最大 10 件を表示します。11 件以上ある場合は矢印キー（↑/↓）でスクロールできます。
**q**・**ESC**・**Ctrl+C** でビューアを終了します。

### 閲覧制限（`passcode-gen config`）

ログを確認できるまでの**遅延時間**と、操作を受け付ける**スケジュール（曜日・時刻帯）**を設定できます。

```sh
# 30分の遅延 + 日曜 9〜11時のみ受付
passcode-gen config set --delay 30m --schedule "日 09:00-11:00"

# スケジュール複数スロット
passcode-gen config set --delay 30m \
  --schedule "月-金 20:00-22:00" \
  --schedule "土,日 09:00-11:00"

# 現在の設定と保留状況を確認
passcode-gen config

# 制限を解除
passcode-gen config disable
```

**スケジュール書式** — `"<曜日> <開始>-<終了>"`:

| 例 | 意味 |
|---|---|
| `"日 09:00-11:00"` | 日曜 9〜11時 |
| `"月-金 20:00-22:00"` | 月〜金 20〜22時 |
| `"土,日 10:00-12:00"` | 土日 10〜12時 |

曜日は英語（`Mon`〜`Sun`）・日本語（`月`〜`日`）どちらも使えます。レンジ（`月-金`）とカンマ（`土,日`）は混在可能です。

**制限の動作**:

1. スケジュールが設定されていてスケジュール外 → **却下**（次の受付時間を表示）
2. 遅延が設定されている場合:
   - 初回実行 → 要求を記録。遅延後・スケジュール内で再実行すると表示
   - タイマー待機中 → 残り時間を表示
   - タイマー切れ → 表示して保留クリア
   - タイマー切れ後、遅延時間と同じ長さを超えて放置 → 要求が**失効**し、新規要求として
     再作成される（log 閲覧・`config set`・`config disable` すべてに適用）
3. 遅延なし・スケジュールのみ → スケジュール内なら即時表示

> **注意:** `~/.passcode-gen/config.bin` を削除すると `log.bin` も永久に復号できなくなります。

リポジトリをクローンして手元で実行・ビルドする場合は次のようにします。

```sh
go run .                 # その場で実行
go build -o passcode-gen # バイナリを生成
./passcode-gen
```

## 短縮コマンド（エイリアス）

`passcode-gen` は名前が長いため、シェルにエイリアスを登録すると短く実行できます。
`~/.zshrc`（bash の場合は `~/.bashrc`）に次の行を追加してください。

```sh
alias pscd-gen='passcode-gen'
```

設定を反映すれば `pscd-gen` で実行できます。

```sh
source ~/.zshrc   # 反映（新しいシェルを開いてもよい）
pscd-gen
```

## 除外するパターン

以下のいずれかに該当する番号は「弱い」とみなし、該当しないものが出るまで再生成します。

| ルール | 内容 | 例 |
| --- | --- | --- |
| 連続する同一桁 | 隣り合う桁が同じ（ゾロ目を含む） | `6000`, `8110`, `0000` |
| 連番 | 昇順・降順の連続 | `1234`, `4321`, `0123` |
| 1桁飛ばしの繰り返し | i 桁目と i+2 桁目が同じ（2つ後ろの桁と一致） | `1212`, `9494`, `0161`, `016010` |
| キーパッド隣接 | テンキー上で隣り合う桁の並び（斜めを含む） | `1236`, `1478` |
| 角→角 | 連続する2桁がともに角キー `1/3/7/9`（対角含む） | `1300`, `1700`, `3700` |
| 2↔0 | 連続する2桁が `2` と `0`（順不同） | `2099`, `8023` |

### キーパッド配置

隣接判定は iPhone のテンキー配置を基準にしています。

```
1 2 3
4 5 6
7 8 9
  0
```

- 隣接 = 上下左右に加え斜めも含む（チェスのキングの動き）
- `0` は `8` の真下とみなし、`8`（真下）・`7`/`9`（斜め）と隣接扱い

## 生成される候補数（4桁の場合）

4桁では全 10000 通り（0000〜9999）のうち、上記ルールをすべて満たす有効な暗証番号は **166 通り** です。
桁数を増やすと有効な組み合わせの総数は増えますが、全体に占める割合（有効率）は下がります
（参考: 有効率は 4桁で約 1.7%、10桁で約 0.0005%）。
ルールが既知だと候補は絞られますが、PIN を使う端末・サービスには通常**試行回数制限**があるため、総当たりに対しては実用上問題ありません。

## テスト

```sh
go test ./...
```

各除外ルールの判定と、全数走査による有効件数（166 通り）の確認を行います。

## ライセンス

[MIT License](LICENSE) で公開しています。
