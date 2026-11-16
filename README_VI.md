<div align="center">

# gitid

**Quản lý, chuyển đổi, clone và kiểm tra danh tính Git trên GitHub, GitLab, Bitbucket, Gitea và Azure DevOps, ngay trong một giao diện terminal.**

[English](README.md) · Tiếng Việt

[![Release](https://img.shields.io/github/v/release/tinhnd1902/gitid?style=flat-square)](https://github.com/tinhnd1902/gitid/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/tinhnd1902/gitid?style=flat-square)](go.mod)
[![License: MIT](https://img.shields.io/github/license/tinhnd1902/gitid?style=flat-square)](LICENSE)
[![CI](https://img.shields.io/github/actions/workflow/status/tinhnd1902/gitid/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/tinhnd1902/gitid/actions/workflows/ci.yml)
[![Stars](https://img.shields.io/github/stars/tinhnd1902/gitid?style=flat-square)](https://github.com/tinhnd1902/gitid/stargazers)

</div>

<!--
Ảnh minh họa: quay bằng vhs (xem CONTRIBUTING.md, mục "Recording the demo"):

    make build && vhs docs/demo.tape      # tạo docs/img/hero.gif

rồi bỏ comment dòng bên dưới. Dòng này được giữ trong comment cho tới khi tệp
tồn tại, để trang không bao giờ hiện ảnh lỗi.

<p align="center"><img src="docs/img/hero.gif" alt="Giao diện terminal của gitid: cây công ty và tài khoản, xem diff và chẩn đoán" width="860"></p>
-->

> 💡 gitid không bọc lệnh `git` và không cài hook vào shell. Nó chỉ ghi các luật `includeIf` thông thường vào cấu hình git, nên chính `git` chọn đúng tên và email, trong mọi công cụ, kể cả khi gitid không chạy.

## Tính năng

- **Tự chọn đúng danh tính:** `user.name` và `user.email` được chọn theo thư mục (`gitdir:`) và theo URL remote (`hasconfig:remote.*.url:`), không cần hook hay cấu hình riêng cho từng repo.
- **Công ty và tài khoản:** nhóm nhiều tài khoản theo công ty (ví dụ 10 công ty, mỗi công ty tới 10 tài khoản), đặt tài khoản mặc định và tìm nhanh bằng bộ lọc mờ (fuzzy).
- **Năm nền tảng:** GitHub, GitLab (kể cả tự host), Bitbucket, Gitea và Azure DevOps (kể cả URL cũ `visualstudio.com`), qua HTTPS hoặc SSH.
- **An toàn mặc định:** mọi thay đổi đều hiện diff trước, tệp được sao lưu trước khi ghi, khối do gitid quản lý có tính idempotent, và `--dry-run` không ghi gì cả.
- **SSH đỡ đau đầu:** mỗi tài khoản một key ed25519 và một alias `Host`, public key chép vào clipboard chỉ với một phím, còn private key không bao giờ bị đọc.
- **Clone và sửa remote:** clone bằng đúng tài khoản với thanh tiến trình, kiểm tra repo đang dùng danh tính nào, và sửa remote sai.
- **Chẩn đoán và nhập cấu hình:** kiểm tra toàn bộ thiết lập với tiến trình từng mục, và bắt đầu nhanh từ các `includeIf` bạn đã có.
- **Hai ngôn ngữ:** tiếng Việt (mặc định) và tiếng Anh, đổi được cho từng lần chạy.

## Bắt đầu nhanh

**Cài qua Homebrew tap (sắp có):**

```bash
# chưa phát hành; hãy dùng một trong các cách bên dưới
# brew install tinhnd1902/tap/gitid
```

**Hoặc dùng `go install`:**

```bash
go install github.com/tinhnd1902/gitid/cmd/gitid@latest
```

Nếu shell báo `command not found: gitid` thì thư mục bin của Go chưa có trong `PATH`. Thêm một lần là xong (ví dụ cho zsh; với bash dùng `~/.bashrc`):

```bash
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

**Hoặc tải bản nhị phân** (macOS và Linux, amd64 và arm64) ở [trang Releases](https://github.com/tinhnd1902/gitid/releases):

```bash
tar -xzf gitid_<phiên-bản>_<hệ-điều-hành>_<kiến-trúc>.tar.gz
install -m 0755 gitid /usr/local/bin/gitid   # hoặc thư mục bất kỳ trong PATH
```

<details>
<summary>Ghi chú cài đặt</summary>

- gitid cần **git 2.36 trở lên** (dùng `hasconfig:remote.*.url`). Kiểm tra bằng `git --version`.
- Bản nhị phân chưa được ký số hay notarize. Nếu macOS chặn tệp tải về, hãy gỡ cờ cách ly một lần: `xattr -d com.apple.quarantine /usr/local/bin/gitid`.
- Kiểm tra tệp tải về bằng `checksums.txt` của bản phát hành: `shasum -a 256 -c checksums.txt --ignore-missing`.
- Bản cài bằng `go install` báo phiên bản là `dev`.
- Tự build từ mã nguồn: `git clone https://github.com/tinhnd1902/gitid && cd gitid && make build` (tệp chạy là `bin/gitid`).

</details>

**Chạy:**

```bash
gitid            # mở TUI; nhấn ? để xem phím tắt, q để thoát
```

**Xem trước an toàn:**

```bash
gitid apply --dry-run     # in diff mọi tệp gitid sẽ đổi, không ghi gì
```

## Cách dùng

```text
gitid                                    # mở giao diện terminal

gitid list [--json]                      # danh sách công ty và tài khoản
gitid check [path] [--json]              # repo này dùng tài khoản và email nào, vì sao
gitid doctor [--ssh-test] [--json]       # chẩn đoán git, ssh, key và cấu hình gitid

gitid clone <url> --account acme/alice   # clone bằng đúng tài khoản (viết lại URL)
      [--dir <thư-mục>] [--dry-run]      #   --dir: thư mục đích, mặc định <thư mục công ty>/<repo>
      [--interactive] [--force]          #   --interactive: cho phép nhập mật khẩu trên terminal
gitid fix-remote [path]                  # sửa URL remote cho khớp tài khoản
      [--account acme/alice]             #   mặc định: tự nhận từ remote
      [--remote origin] [--dry-run]

gitid key gen acme/carol                 # tạo key ed25519 cho tài khoản SSH
      [--no-passphrase] [--yes]          #   passphrase do chính ssh-keygen hỏi nên cần terminal
      [--copy]                           #   chép public key (mặc định: chỉ khi chạy trên terminal)

gitid apply [--dry-run] [--yes]          # ghi thay đổi vào ~/.gitconfig và ~/.ssh (có sao lưu)
gitid import [--dry-run]                 # đề xuất công ty từ các includeIf sẵn có
      [--select initech --platform github --username alice --auth https --host <host>]

gitid version                            # in phiên bản
gitid completion <bash|zsh|fish|powershell>   # tự hoàn thành lệnh
```

Mã thoát: `0` ổn, `1` lỗi, `2` `check` hoặc `doctor` phát hiện vấn đề (tiện cho script).

```bash
# Xem trước mọi thứ, chỉ áp dụng sau khi đã đọc diff
gitid apply --dry-run
gitid apply                                  # hỏi "y/N" trên terminal
gitid clone git@github.com:org/repo.git --account acme/alice --dry-run
gitid fix-remote --dry-run                   # hiện cũ -> mới, không đổi gì
```

`gitid apply` từ chối chạy khi không có terminal, trừ khi bạn thêm `--yes`, nên script không thể ghi tệp ngoài ý muốn. `gitid key gen` từ chối tạo key có passphrase khi stdin không phải terminal (ssh-keygen sẽ coi EOF là passphrase rỗng); trong script phải chủ động dùng `--no-passphrase --yes`. Lệnh chỉ động tới clipboard khi chạy trên terminal hoặc có `--copy`.

`gitid check --json` in một đối tượng. `warnings` là danh sách `{code, message}`: script nên so khớp theo `code` ổn định, còn `message` là văn bản đã dịch (theo ngôn ngữ đang dùng) để hiển thị cho người.

```json
{
  "repo": "/home/me/Code/acme/demo",
  "remote": "https://github.com/acme-org/demo.git",
  "effective_email": "alice@example.com",
  "email_origin": "file:/home/me/.config/gitid/git/acme-alice.gitconfig",
  "account": "acme/alice",
  "reason": "default",
  "expected_email": "alice@example.com",
  "warnings": [
    {
      "code": "warn.remote_no_username",
      "message": "Remote không có username nên git không biết chọn tài khoản nào ngoài thư mục công ty."
    }
  ]
}
```

## Cách hoạt động

gitid lưu tài khoản trong `config.yaml` riêng của nó, rồi sinh cấu hình git thông thường từ đó. Việc chọn danh tính do git đảm nhiệm:

```text
~/.gitconfig
├─ [user]  (của bạn, giữ nguyên: là giá trị dự phòng)
└─ # >>> gitid managed block >>>
   ├─ [includeIf "gitdir:~/Code/acme/"]  ──▶  ~/.config/gitid/git/acme.gitconfig
   │                                            ├─ [include]  acme-alice.gitconfig        tài khoản mặc định của công ty
   │                                            └─ [includeIf "hasconfig:remote.*.url:…"]
   │                                                 acme-bob.gitconfig, acme-carol.gitconfig   ghi đè theo từng tài khoản
   └─ [includeIf "hasconfig:remote.*.url:git@github-acme-carol:*/**"]   ngoài thư mục công ty
      (chỉ các pattern xác định đúng một tài khoản)
   # <<< gitid managed block <<<
```

Thứ tự ưu tiên, từ yếu tới mạnh (giá trị git đọc sau cùng sẽ thắng):

1. `[user]` toàn cục của bạn là giá trị dự phòng.
2. Trong thư mục của một công ty, **tài khoản mặc định** của công ty được dùng.
3. Cũng trong thư mục đó, tài khoản có **pattern URL remote** khớp với remote của repo sẽ ghi đè mặc định. Vì vậy cùng một username có thể tồn tại ở hai công ty mà không đụng nhau: việc so khớp diễn ra bên trong tệp gắn với thư mục.
4. Ngoài mọi thư mục công ty, chỉ các pattern trỏ tới đúng một tài khoản được dùng: alias SSH luôn duy nhất, còn pattern HTTPS `user@host` chỉ được dùng khi không tài khoản nào khác sinh ra cùng pattern. `gitid doctor` báo các pattern mơ hồ, và trường `orgs` của tài khoản giúp thu hẹp chúng.
5. `user.name` hoặc `user.email` nằm trong `.git/config` của chính repo thắng tất cả các mức trên. `gitid check` cảnh báo khi điều đó xảy ra.

Hai chế độ xác thực trong thực tế:

| Chế độ    | URL remote                          | gitid thiết lập gì                                                                                              |
| --------- | ----------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| **HTTPS** | `https://alice@github.com/org/repo` | Username nằm trong URL để credential helper chọn đúng tài khoản đăng nhập. gitid không bao giờ lưu mật khẩu.       |
| **SSH**   | `git@github-acme-carol:org/repo`    | Alias `Host github-acme-carol` trong `~/.ssh/config.d/gitid.conf` với key riêng và `IdentitiesOnly yes`.          |

## An toàn

gitid sửa tệp trong thư mục home của bạn, nên nó rất chặt chẽ:

- **Chạm vào những gì:** khối do gitid quản lý trong `~/.gitconfig`; khối do gitid quản lý (một dòng `Include config.d/*`) ở đầu `~/.ssh/config`; `~/.ssh/config.d/gitid.conf`; các key bạn yêu cầu tạo trong `~/.ssh/gitid/`; và thư mục riêng `$XDG_CONFIG_HOME/gitid/` (mặc định `~/.config/gitid/`) chứa `config.yaml`, các tệp `git/*.gitconfig` được sinh ra và `backups/`. Không có gì khác.
- **Khối được quản lý:** mọi thứ gitid ghi vào tệp của bạn nằm giữa `# >>> gitid managed block … >>>` và `# <<< gitid managed block <<<`. Phần nằm ngoài hai dấu đó không bao giờ bị đổi. Chạy `apply` lần thứ hai không đổi gì và không tạo bản sao lưu mới.
- **Xem trước và xác nhận:** TUI luôn hiện diff và hỏi trước khi ghi. `gitid apply --dry-run` in cùng diff đó và không tạo gì, kể cả thư mục.
- **Sao lưu:** trước mọi lần ghi, nội dung cũ của từng tệp bị ảnh hưởng được chép vào `~/.config/gitid/backups/<mốc-thời-gian>/` (thư mục `0700`, tệp `0600`) cùng `manifest.json` ghi đường dẫn gốc, quyền và SHA-256. 30 bản sao lưu mới nhất được giữ lại.
- **Ghi nguyên tử và cẩn thận:** mỗi tệp được ghi vào tệp tạm cùng thư mục, flush rồi đổi tên. Symlink (ví dụ do công cụ quản lý dotfiles) được theo dõi để vẫn là symlink, quyền tệp được giữ nguyên, và kế hoạch được kiểm tra lại ngay trước khi ghi: nếu một tệp đích đã đổi từ lúc tạo diff thì không ghi gì cả. Khóa tiến trình ngăn hai gitid áp dụng cùng lúc, và nếu có `~/.gitconfig.lock` thì dừng.
- **Private key:** gitid không bao giờ đọc, in, sửa hay ghi đè private key. Nó chỉ kiểm tra tệp có tồn tại không và quyền truy cập; nó đọc tệp `.pub`. Xóa tài khoản sẽ giữ key, trừ khi bạn gõ id tài khoản để xác nhận xóa, và chỉ xóa được key nằm trong `~/.ssh/gitid/`.
- **Bí mật trong URL:** mật khẩu hoặc token dán trong URL bị loại bỏ và che đi ở mọi nơi có thể hiển thị.
- **Chống chèn lệnh:** tên, email, host và username được kiểm tra và escape trước khi vào cấu hình git hay ssh; lệnh clone luôn dùng `git clone -- <url> <thư mục>`.
- **Mạng:** không dùng, trừ khi bạn clone hoặc tự bấm bài thử `ssh -T`. Không có telemetry.
- **Gỡ bỏ:** xóa hết công ty trong TUI rồi áp dụng; tệp của bạn trở về đúng từng byte ban đầu. Hoặc khôi phục thủ công (bên dưới).

<details>
<summary>Khôi phục thủ công</summary>

Mỗi lần `apply` có thay đổi tệp sẽ tạo một thư mục như `~/.config/gitid/backups/20261116-140102.123/`:

```text
manifest.json              # thời điểm tạo, và với mỗi tệp: path, op, mode, sha256, backup_file
000-gitconfig              # nội dung cũ của ~/.gitconfig (nếu có)
001-config                 # nội dung cũ của ~/.ssh/config
...
```

Để hoàn tác, tìm mục tương ứng trong `manifest.json` rồi chép `backup_file` đè lên `path`, ví dụ:

```bash
cp ~/.config/gitid/backups/<mốc-thời-gian>/000-gitconfig ~/.gitconfig
```

Mục có `"existed": false` là tệp do lần chạy đó tạo ra: xóa chúng là quay lại trạng thái cũ. Các tệp trong `~/.config/gitid/git/` do gitid sinh ra và có thể xóa thẳng.

</details>

## Cấu hình

gitid đọc và ghi một tệp cấu hình:

| Mục                   | Vị trí                                                                       |
| --------------------- | ---------------------------------------------------------------------------- |
| Tệp cấu hình          | `$XDG_CONFIG_HOME/gitid/config.yaml` (quyền `0600`)                          |
| Mặc định khi XDG chưa đặt, rỗng hoặc không phải đường dẫn tuyệt đối | `~/.config/gitid/config.yaml` |
| Tệp git được sinh ra  | `<thư mục cấu hình>/git/`                                                    |
| Sao lưu               | `<thư mục cấu hình>/backups/`                                                |

Biến môi trường: `XDG_CONFIG_HOME` (vị trí cấu hình, mặc định `~/.config`) và `GITID_LANG` (`vi` hoặc `en`; ưu tiên hơn `settings.language`).

```yaml
version: 1
settings:
  language: vi              # vi (mặc định) hoặc en
  code_root: ~/Code         # thư mục cha gợi ý cho công ty mới
companies:
  - slug: acme              # a-z, 0-9, dấu gạch ngang; duy nhất; không đổi được sau này
    name: ACME Corp
    base_dir: ~/Code/acme/  # repo bên dưới thư mục này dùng tài khoản của công ty
    default_account: alice
    accounts:
      - id: alice           # duy nhất trong công ty; không đổi được sau này
        platform: github    # github | gitlab | bitbucket | gitea | azure-devops
        username: alice
        name: Alice Example # user.name của git
        email: alice@example.com
        auth: https         # https | ssh
        orgs: [acme-org]    # tùy chọn: chỉ remote thuộc các org này mới khớp tài khoản
      - id: carol
        platform: github
        username: carol
        name: Carol Example
        email: carol@example.com
        auth: ssh
        ssh_key: ~/.ssh/gitid/acme-carol   # được đặt khi gitid tạo key
  - slug: globex
    name: Globex
    base_dir: ~/Code/globex/
    accounts:
      - id: dave
        platform: gitlab
        host: git.example.com   # tự host; để trống = host mặc định của nền tảng
        ssh_port: 2222          # tùy chọn
        username: dave
        name: Dave Example
        email: dave@globex.example
        auth: ssh
```

Mỗi lần lưu, gitid ghi lại `config.yaml` từ mô hình nội bộ, nên **chú thích bạn tự thêm bằng tay sẽ không được giữ**. Tệp cũ được sao lưu trước. Slug của công ty và id của tài khoản không đổi được sau khi tạo (chúng đặt tên cho alias SSH, tệp key và các tệp sinh ra); hãy xóa rồi tạo lại.

## Tính năng chi tiết

### Danh sách

Màn hình chính là cây công ty và tài khoản với thẻ nền tảng (`GH`, `GL`, `BB`, `GT`, `AZ`), kiểu xác thực, `★` cho tài khoản mặc định của công ty và email. Nhấn `/` để lọc theo tên, username, email, host hoặc nền tảng; việc so khớp là mờ trong từng từ và giữ nguyên thứ tự của cây. Trên terminal rộng (từ 100 cột) có khung chi tiết cho dòng đang chọn; còn không thì nhấn `enter` để mở.

```text
▌ acme  ACME Corp                            ~/Code/acme/  2
 ├─ ★ alice      GH https                  alice@example.com
 └─   carol      GH ssh                    carol@example.com
▪ globex  Globex                           ~/Code/globex/  1
 └─ ★ dave       GL ssh                  dave@globex.example

● 6 thay đổi chưa áp dụng (nhấn p)
```

### Thêm và sửa tài khoản

`A` thêm công ty, `a` thêm tài khoản vào công ty đang chọn, `e` sửa, `d` xóa. Biểu mẫu kiểm tra dữ liệu ngay khi nhập, theo đúng các luật mà CLI dùng. Nếu chọn SSH, gitid đề nghị tạo key ngay sau khi lưu. Sau mỗi thay đổi bạn xem diff rồi xác nhận.

### Clone

`c` hỏi URL, xếp hạng các tài khoản (tài khoản cùng host lên trước), xem trước URL đã viết lại và thư mục đích, rồi clone ở chế độ nền với thanh tiến trình. Xong, gitid kiểm tra email mà git sẽ dùng trong repo mới.

```text
URL gốc:   https://github.com/org/repo.git
URL clone:  https://alice@github.com/org/repo.git
Thư mục:    ~/Code/acme/repo
✓ Đã clone xong
✓ user.email trong repo: alice@example.com (đúng).
```

Nếu xác thực thất bại ở chế độ nền, gitid đề nghị chạy lại ngay trên terminal, nơi bạn nhập được mật khẩu hoặc passphrase.

### Kiểm tra và sửa remote

`k` kiểm tra repo ở thư mục hiện tại: tài khoản khớp, email git thực sự dùng và email đó đến từ tệp nào. Nó cảnh báo khi repo tự ghi đè email, khi không khớp hoặc khớp mơ hồ, và khi cấu hình chưa được áp dụng. Nếu remote không dùng đúng tài khoản, `f` hiện thay đổi và hỏi trước khi chạy `git remote set-url`.

```text
Repo:       ~/Code/acme/demo
Remote:     https://github.com/acme-org/demo.git
Tài khoản:  acme/alice  (tài khoản mặc định của thư mục)
Mong đợi:   alice@example.com
Đang dùng:  alice@example.com  (file:~/.config/gitid/git/acme-alice.gitconfig)
! Remote không có username nên git không biết chọn tài khoản nào ngoài thư mục công ty.

Sửa remote origin theo tài khoản acme/alice
- https://github.com/acme-org/demo.git
+ https://alice@github.com/acme-org/demo.git
```

### Chẩn đoán (doctor)

`D` chạy mọi kiểm tra cùng lúc, mỗi mục một spinner riêng: phiên bản git, cấu hình hợp lệ, credential helper, `Include` của ssh, `gitid.conf`, key và quyền truy cập, thay đổi chưa áp dụng, thư mục gốc, trùng alias và pattern mơ hồ. Nhấn `s` để thử thêm `ssh -T` tới từng alias (đây là bước duy nhất dùng mạng, và chỉ khi bạn nhấn).

```text
✓ Phiên bản git      git 2.56.0
✓ Cấu hình gitid
! Credential helper
  ! Chỉ có osxkeychain; chưa có Git Credential Manager.
      → Cài: brew install --cask git-credential-manager
✗ SSH key
  ✗ Thiếu key của acme/carol (~/.ssh/gitid/acme-carol).
      → Chạy `gitid key gen <công-ty>/<tài-khoản>`.
```

<details>
<summary>`gitid key gen` làm chính xác những gì</summary>

Nó dừng lại nếu tệp private key hoặc public key đã tồn tại. Sau đó chạy `ssh-keygen -t ed25519 -a 100 -C "<email> gitid:<công-ty>/<tài-khoản>" -f ~/.ssh/gitid/<công-ty>-<tài-khoản>` gắn trực tiếp vào terminal của bạn, nên **ssh-keygen tự hỏi passphrase và gitid không bao giờ nhìn thấy nó**. `--no-passphrase` (hoặc bước xác nhận thứ hai trong TUI) là cách duy nhất để dùng passphrase rỗng. Cuối cùng gitid kiểm tra cả hai tệp đã có, ghi `ssh_key` vào cấu hình, hiện public key (`c` chép bằng `pbcopy`) và trang mà nền tảng yêu cầu thêm key (`o` để mở).

</details>

### Nhập cấu hình (import)

`i` (hoặc `gitid import`) đọc các mục `includeIf "gitdir:…"` trong `~/.gitconfig` cùng phần `[user]` của các tệp chúng trỏ tới, rồi đề xuất một công ty cho mỗi mục. Nó không đoán nền tảng hay username: bạn tự nhập, rồi xem diff. Các host tìm thấy trong `~/.ssh/config` chỉ được liệt kê để tham khảo; key của chúng không bị mở.

## Xử lý sự cố

<details>
<summary>Repo dùng sai tài khoản hoặc email</summary>

Chạy `gitid check` trong repo. Lệnh in `user.email` đang có hiệu lực, tệp nó đến từ đâu và tài khoản gitid mong đợi. Nguyên nhân thường gặp: repo có `user.email` riêng trong `.git/config` (gỡ bằng `git config --local --unset user.email`); remote không có username nên không gì xác định được tài khoản bên ngoài thư mục công ty (chạy `gitid fix-remote`); hoặc cấu hình chưa được áp dụng (`gitid apply`).

</details>

<details>
<summary>"Too many authentication failures" khi dùng SSH</summary>

ssh-agent đưa ra mọi key đã nạp trước khi tới key đúng. Các khối `Host` của gitid đặt `IdentitiesOnly yes`, nên hãy dùng remote dạng alias (`git@github-acme-carol:org/repo.git`; `gitid fix-remote` sẽ viết lại giúp bạn). Nếu có khối `Host *` trong `~/.ssh/config` của bạn thêm danh tính, dòng `Include` của gitid ở đầu tệp vẫn được ưu tiên cho các alias của nó. Kiểm tra bằng `ssh -vT git@<alias>` và `gitid doctor --ssh-test`.

</details>

<details>
<summary>Thiếu Git Credential Manager</summary>

Với tài khoản HTTPS, cần một credential helper nhớ riêng từng username. gitid không bao giờ tự cài phần mềm; `gitid doctor` chỉ gợi ý. Trên macOS: `brew install --cask git-credential-manager`. Helper có sẵn `osxkeychain` cũng dùng được khi mỗi host chỉ có một tài khoản.

</details>

<details>
<summary>"git quá cũ" hoặc các include bị bỏ qua</summary>

`hasconfig:remote.*.url` cần **git 2.36 trở lên**. Hãy nâng cấp git (trên macOS: `brew install git`) và bảo đảm bản mới đứng đầu trong `PATH`; `gitid doctor` in ra phiên bản nó tìm thấy.

</details>

<details>
<summary>Terminal hiển thị lỗi hoặc quá nhỏ</summary>

gitid cần tối thiểu 40x10 ô và đẹp nhất từ 80x24; terminal rộng (từ 100 cột) có thêm khung chi tiết. Nếu màn hình bị rối sau khi chương trình sập, chạy `reset`. TUI chỉ chạy trên terminal thật; trong script hãy dùng các lệnh con.

</details>

## Đóng góp

Rất hoan nghênh đóng góp: fork repo, tạo nhánh và mở pull request. Xem [CONTRIBUTING.md](CONTRIBUTING.md) để biết các lệnh make và quy tắc kiểm thử (test không bao giờ chạm vào HOME thật của bạn). Vui lòng đọc [Quy tắc ứng xử](CODE_OF_CONDUCT.md), và báo lỗ hổng bảo mật theo cách riêng tư như trong [SECURITY.md](SECURITY.md).

## Giấy phép

[MIT](LICENSE) © 2026 tinhnd1902
