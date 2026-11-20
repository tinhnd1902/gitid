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
- **Trình duyệt repo:** một màn hình có tab Trên máy (mọi repo trong thư mục các công ty, tài khoản mà remote khớp và điều gì đang sai), tab URL (clone) và tab Online (repo mà từng tài khoản thấy trên GitHub, GitLab, Gitea, Bitbucket Cloud hoặc Azure DevOps, nếu bạn có token cho nó).
- **Chẩn đoán và nhập cấu hình:** kiểm tra toàn bộ thiết lập với tiến trình từng mục, và bắt đầu nhanh từ các `includeIf` bạn đã có.
- **Trang chủ:** `gitid` mở một menu đánh số các việc hằng ngày (tài khoản, clone, kiểm tra repo, duyệt repo, áp dụng, chẩn đoán, nhập từ git, cài đặt); bấm `1` đến `8` để vào thẳng một việc. Menu cũng báo bằng một dòng khi có bản mới (xem [Mạng](#an-toàn)).
- **Hai ngôn ngữ:** tiếng Việt (mặc định) và tiếng Anh. Đổi trong Cài đặt (trang chủ, bấm `8`): đổi có hiệu lực ngay và được lưu vào `settings.language`. `GITID_LANG` vẫn được ưu tiên khi gitid khởi động.

## Bắt đầu nhanh

**1. Homebrew (macOS):**

```bash
brew install tinhnd1902/tap/gitid
```

**2. Script cài đặt (macOS và Linux, amd64 và arm64):**

```bash
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | sh
```

Script tải bản phát hành mới nhất, kiểm tra sha256 với `checksums.txt` của bản đó, cài `gitid` (vào `/usr/local/bin` nếu bạn ghi được ở đó, nếu không thì vào `~/.local/bin`) và, khi thư mục đó chưa có trong `PATH`, thêm nó vào tệp khởi động của shell. Sau đó hãy mở một terminal mới. Tùy chọn là các biến môi trường:

```bash
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | VERSION=0.1.0 sh          # một bản phát hành cụ thể
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | INSTALL_DIR=$HOME/bin sh   # chọn thư mục (phải ghi được)
curl -fsSL https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh | NO_MODIFY_PATH=1 sh        # không bao giờ sửa tệp khởi động của shell
```

Muốn đọc script trước khi chạy?

```bash
curl -fsSLo install.sh https://raw.githubusercontent.com/tinhnd1902/gitid/main/install.sh
less install.sh
sh install.sh
```

**3. Hoặc dùng `go install`:**

```bash
go install github.com/tinhnd1902/gitid/cmd/gitid@latest
```

Nếu shell báo `command not found: gitid` thì thư mục bin của Go chưa có trong `PATH`. Thêm một lần là xong (ví dụ cho zsh; với bash dùng `~/.bashrc`):

```bash
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

**4. Hoặc tải bản nhị phân** (macOS và Linux, amd64 và arm64) ở [trang Releases](https://github.com/tinhnd1902/gitid/releases):

```bash
tar -xzf gitid_<phiên-bản>_<hệ-điều-hành>_<kiến-trúc>.tar.gz
install -m 0755 gitid /usr/local/bin/gitid   # hoặc thư mục bất kỳ trong PATH
```

<details>
<summary>Ghi chú cài đặt</summary>

- gitid cần **git 2.36 trở lên** (dùng `hasconfig:remote.*.url`). Kiểm tra bằng `git --version`.
- Bản nhị phân chưa được ký số hay notarize. Homebrew đánh dấu cách ly (quarantine) cho cask tải về, nên cask gitid gỡ cờ đó khỏi riêng tệp `gitid` (bước `postflight` chạy `xattr -dr com.apple.quarantine`). Việc này bỏ qua kiểm tra Gatekeeper cho đúng một tệp này; bản tải về vẫn được kiểm tra bằng sha256 ghi trong cask. Nếu bạn muốn giữ Gatekeeper, hãy dùng script cài đặt hoặc `go install`. Với bản nhị phân tự tải về mà macOS chặn, hãy gỡ cờ cách ly một lần: `xattr -d com.apple.quarantine /usr/local/bin/gitid`.
- Cài qua Homebrew là cask nên chỉ dành cho macOS. Trên Linux hãy dùng script cài đặt.
- Script cài đặt ghi một tệp (`gitid`) và, khi cần, thêm một dòng có chú thích `# added by gitid installer` vào một tệp khởi động của shell (`~/.zshrc`, `~/.bash_profile` trên macOS hoặc `~/.bashrc` trên Linux, `~/.config/fish/conf.d/gitid.fish`, hoặc `~/.profile`). Nó không bao giờ dùng `sudo`, không gửi telemetry và chỉ kết nối tới GitHub. Checksum bảo vệ khỏi bản tải bị hỏng hoặc bị can thiệp; nó không chống được trường hợp chính bản phát hành bị xâm phạm, vì cả hai tệp đến từ cùng một nơi.
- Kiểm tra tệp tải về bằng `checksums.txt` của bản phát hành: `shasum -a 256 -c checksums.txt --ignore-missing`.
- Bản cài bằng `go install` báo phiên bản là `dev`.
- Tự build từ mã nguồn: `git clone https://github.com/tinhnd1902/gitid && cd gitid && make build` (tệp chạy là `bin/gitid`).

</details>

**Chạy:**

```bash
gitid            # mở trang chủ; bấm 1-8 để chọn việc, ? để xem phím tắt, q để thoát
```

Lần đầu dùng TUI: bấm `1` để mở Tài khoản (trang chủ ghi «Bắt đầu ở đây: thêm công ty đầu tiên»), rồi làm ba bước (màn hình cũng hiện sẵn các bước, và dòng dưới cùng luôn đánh dấu phím tiếp theo bằng `▸`):

1. `A` thêm công ty: một slug ngắn và một thư mục gốc như `~/Code/acme/`.
2. `a` thêm tài khoản Git (username, email) cho công ty đó. Form hiện sẵn remote tài khoản sẽ dùng (`https://<username>@github.com/…`) và ID gitid tự đặt theo username. Username là tên đăng nhập của bạn trên nền tảng (phần trong `github.com/<login>`), không phải email; email chỉ để ghi tác giả commit và gitid không xác minh nó.
3. `p` xem trước các tệp gitid sẽ ghi, rồi `y` để áp dụng (mọi tệp đều được sao lưu trước). Từ đó mọi repo trong thư mục gốc tự dùng đúng tài khoản: `c` clone repo vào đó, `k` kiểm tra một repo. Lần đầu clone hoặc push qua HTTPS, git sẽ hỏi đăng nhập (nếu hỏi mật khẩu, hãy dùng Personal Access Token); bản thân gitid không đăng nhập thay bạn. Việc duy nhất nó làm với nền tảng là tab Online của trình duyệt repo: chỉ đọc một token bạn đã có (xem [Tab Online và token](#tab-online-và-token)) và chỉ khi bạn mở tab đó.

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

gitid repos [--company acme] [--depth 4] # mọi repo trong thư mục các công ty và điều gì đang sai
      [--problems] [--json]              #   thoát với mã 2 khi có gì cần xử lý
gitid repos --online [--account acme/alice] [--max-pages 50] [--local] [--json]
                                         # những gì mỗi tài khoản thấy trên nền tảng (cần token)
gitid token set acme/alice               # lưu token (đọc từ stdin) vào Keychain của macOS
gitid token rm acme/alice                # xóa nó
gitid token status [acme/alice] [--json] # token của từng tài khoản lấy từ đâu (không bao giờ in token)

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

gitid keys [--json]                      # phím tắt, gồm phím tùy chỉnh trong keys.yaml và các vấn đề tìm thấy
gitid keys --example                     # in mẫu keys.yaml đã comment sẵn (không ghi tệp nào)

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

- **Chạm vào những gì:** khối do gitid quản lý trong `~/.gitconfig`; khối do gitid quản lý (một dòng `Include config.d/*`) ở đầu `~/.ssh/config`; `~/.ssh/config.d/gitid.conf`; các key bạn yêu cầu tạo trong `~/.ssh/gitid/`; và thư mục riêng `$XDG_CONFIG_HOME/gitid/` (mặc định `~/.config/gitid/`) chứa `config.yaml`, các tệp `git/*.gitconfig` được sinh ra, `backups/` và, sau khi kiểm tra bản mới chạy, `update-check.json` (thời điểm kiểm tra gần nhất và phiên bản mới nhất nó thấy; gitid không bao giờ tạo thư mục chỉ để ghi tệp này). Không có gì khác, trừ một ngoại lệ do bạn chủ động chọn: `gitid token set` thêm một mục cho mỗi tài khoản vào Keychain của macOS (service `gitid`; xem [Tab Online và token](#tab-online-và-token)). gitid cũng đọc tệp `keys.yaml` tùy chọn ở đó (phím tắt của bạn) nhưng không bao giờ ghi vào nó.
- **Khối được quản lý:** mọi thứ gitid ghi vào tệp của bạn nằm giữa `# >>> gitid managed block … >>>` và `# <<< gitid managed block <<<`. Phần nằm ngoài hai dấu đó không bao giờ bị đổi. Chạy `apply` lần thứ hai không đổi gì và không tạo bản sao lưu mới.
- **Xem trước và xác nhận:** TUI luôn hiện diff và hỏi trước khi ghi. `gitid apply --dry-run` in cùng diff đó và không tạo gì, kể cả thư mục.
- **Sao lưu:** trước mọi lần ghi, nội dung cũ của từng tệp bị ảnh hưởng được chép vào `~/.config/gitid/backups/<mốc-thời-gian>/` (thư mục `0700`, tệp `0600`) cùng `manifest.json` ghi đường dẫn gốc, quyền và SHA-256. 30 bản sao lưu mới nhất được giữ lại.
- **Ghi nguyên tử và cẩn thận:** mỗi tệp được ghi vào tệp tạm cùng thư mục, flush rồi đổi tên. Symlink (ví dụ do công cụ quản lý dotfiles) được theo dõi để vẫn là symlink, quyền tệp được giữ nguyên, và kế hoạch được kiểm tra lại ngay trước khi ghi: nếu một tệp đích đã đổi từ lúc tạo diff thì không ghi gì cả. Khóa tiến trình ngăn hai gitid áp dụng cùng lúc, và nếu có `~/.gitconfig.lock` thì dừng.
- **Private key:** gitid không bao giờ đọc, in, sửa hay ghi đè private key. Nó chỉ kiểm tra tệp có tồn tại không và quyền truy cập; nó đọc tệp `.pub`. Xóa tài khoản sẽ giữ key, trừ khi bạn gõ id tài khoản để xác nhận xóa, và chỉ xóa được key nằm trong `~/.ssh/gitid/`.
- **Bí mật trong URL:** mật khẩu hoặc token dán trong URL bị loại bỏ và che đi ở mọi nơi có thể hiển thị.
- **Chống chèn lệnh:** tên, email, host và username được kiểm tra và escape trước khi vào cấu hình git hay ssh; lệnh clone luôn dùng `git clone -- <url> <thư mục>`.
- **Quét repo:** tab Trên máy của trình duyệt repo chỉ đọc: nó liệt kê thư mục trong các thư mục gốc của công ty và chạy một lệnh `git config` chỉ đọc trong mỗi repo tìm được. Nó không ghi gì; bản thân gitid không mở tệp nào bên trong repo, trừ tệp `.git` một dòng của worktree hay submodule (git tự đọc cấu hình của repo).
- **Mạng:** một yêu cầu tự động, cộng ba chỗ chỉ chạy khi bạn yêu cầu. Yêu cầu tự động là **kiểm tra bản mới**: khi TUI mở, gitid gửi đúng một yêu cầu `GET https://github.com/tinhnd1902/gitid/releases/latest` không xác thực để xem có bản mới không, nhiều nhất mỗi 24 giờ (thời điểm lần thử gần nhất và phiên bản mới nhất đã thấy được giữ trong `~/.config/gitid/update-check.json`, nên lần thử lỗi cũng không lặp lại trong một ngày). Yêu cầu không có token, cookie, query hay body, chỉ mang `User-Agent: gitid/<phiên bản>`. gitid chỉ đọc header `Location` của phản hồi, không theo chuyển hướng, không đọc body, chỉ chấp nhận khi nó trỏ tới `github.com/tinhnd1902/gitid/releases/tag/vX.Y.Z`, và chỉ hiển thị số phiên bản. Nó dùng cùng cài đặt proxy và chứng chỉ với tab Online, bỏ cuộc sau 5 giây mà không làm chậm màn hình đầu tiên, và lỗi thì im lặng. Nó chỉ chạy khi TUI mở (không bao giờ với `gitid list`, `apply` hay lệnh khác) và không chạy với bản không phải phát hành thường (`dev`, snapshot, bản thử). Để tắt, đặt `GITID_NO_UPDATE_CHECK=1` (mọi giá trị trừ rỗng, `0` và `false`): khi đó gitid không đọc, không ghi gì cho việc này và không gửi yêu cầu nào. Ba chỗ chạy khi bạn yêu cầu là: clone, bài thử `ssh -T` tùy chọn, và tab Online của trình duyệt repo hoặc `gitid repos --online` (chỉ khi bạn mở hoặc chạy nó), nơi gửi các yêu cầu `GET` chỉ đọc tới API của host các tài khoản của bạn. Với tài khoản Azure DevOps không có `orgs`, token đó còn được gửi tới `app.vssps.visualstudio.com`, dịch vụ của Microsoft liệt kê các organization của bạn. Không có telemetry.
- **Token (tab Online):** đọc từ biến môi trường, mục Keychain của gitid, `gh`/`glab` hoặc credential helper của git; chỉ giữ trong bộ nhớ, không bao giờ ghi ra đĩa, không bao giờ nằm trong dòng lệnh, URL, log hay lỗi, và chỉ được gửi dưới dạng header `Authorization` tới API của host chính tài khoản đó qua https (không theo chuyển hướng hay liên kết trang kế sang host khác; không bao giờ tắt kiểm tra chứng chỉ). Chỗ duy nhất gitid ghi token là mục Keychain bạn yêu cầu bằng `gitid token set`: token được đọc từ stdin (gõ không hiện ký tự khi ở terminal) và đưa cho công cụ `security` của macOS qua đầu vào chuẩn của nó, không bao giờ qua dòng lệnh, nên không hiện trong `ps`. gitid không bao giờ đổi những gì credential helper lưu.
- **Gỡ bỏ:** xóa hết công ty trong TUI rồi áp dụng; tệp của bạn trở về đúng từng byte ban đầu. Hoặc khôi phục thủ công (bên dưới). Để gỡ chính chương trình gitid, xem [Gỡ cài đặt chương trình](#gỡ-cài-đặt-chương-trình).

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

gitid đọc và ghi một tệp cấu hình, và đọc thêm một tệp phím tắt tùy chọn:

| Mục                   | Vị trí                                                                       |
| --------------------- | ---------------------------------------------------------------------------- |
| Tệp cấu hình          | `$XDG_CONFIG_HOME/gitid/config.yaml` (quyền `0600`)                          |
| Mặc định khi XDG chưa đặt, rỗng hoặc không phải đường dẫn tuyệt đối | `~/.config/gitid/config.yaml` |
| Phím tắt (tùy chọn, chỉ đọc) | `<thư mục cấu hình>/keys.yaml`, xem [Tùy chỉnh phím tắt](#tùy-chỉnh-phím-tắt) |
| Tệp git được sinh ra  | `<thư mục cấu hình>/git/`                                                    |
| Sao lưu               | `<thư mục cấu hình>/backups/`                                                |

Biến môi trường: `XDG_CONFIG_HOME` (vị trí cấu hình, mặc định `~/.config`) và `GITID_LANG` (`vi` hoặc `en`; ưu tiên hơn `settings.language` mỗi lần gitid khởi động). Màn hình Cài đặt (trang chủ, bấm `8`) đổi `settings.language` giúp bạn; nếu `GITID_LANG` đang được đặt, màn hình nói rõ và lần mở sau gitid dùng lại biến đó.

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

### Tùy chỉnh phím tắt

Giao diện terminal dùng được ngay, không cần thiết lập. Muốn đổi phím tắt, tạo tệp tùy chọn `keys.yaml` cùng thư mục với `config.yaml`: `$XDG_CONFIG_HOME/gitid/keys.yaml`, tức `~/.config/gitid/keys.yaml` khi `XDG_CONFIG_HOME` chưa đặt, rỗng hoặc không phải đường dẫn tuyệt đối (cùng quy tắc với `config.yaml`).

**gitid chỉ đọc tệp này.** gitid không bao giờ tạo, ghi lại, sao lưu hay xóa `keys.yaml`, nên chú thích của bạn được giữ nguyên, và sửa tệp này không bao giờ làm một lần lưu `config.yaml` bị coi là «cũ». `config.yaml` không có mục `keys:` (gitid từ chối mục đó).

```sh
gitid keys                                          # mọi hành động cùng phím của nó, và các vấn đề trong keys.yaml
mkdir -p ~/.config/gitid && gitid keys --example > ~/.config/gitid/keys.yaml    # mẫu với mọi dòng đã comment sẵn
```

Thư mục này chưa tồn tại trước khi gitid lưu `config.yaml` lần đầu, nên cần `mkdir -p`. Nếu bạn đặt `XDG_CONFIG_HOME` là đường dẫn tuyệt đối, dùng `$XDG_CONFIG_HOME/gitid/` thay cho `~/.config/gitid/`.

`gitid keys --example` chỉ in ra màn hình; phép chuyển hướng ở trên mới tạo tệp, và nó **ghi đè `keys.yaml` đang có**. Bỏ dấu `#` ở đầu những dòng bạn muốn đổi.

Tệp là một bảng `hành_động: "phím"` hoặc `hành_động: ["phím", "phím"]` (thanh phím và các gợi ý hiển thị phím đầu tiên; bảng `?` hiển thị tất cả, ví dụ `c/n`):

```yaml
check: "v"          # v thay cho k
clone: ["c", "n"]   # hai phím cho một hành động
```

| Hành động | Mặc định | Dùng ở |
| --------- | -------- | ------ |
| `quit` | `q` | trang chủ |
| `help` | `?` | trang chủ, mọi màn hình trừ biểu mẫu |
| `back` | `q` (`esc` luôn quay lại) | mọi màn hình trừ trang chủ (kể cả danh sách Tài khoản: nó quay về trang chủ) |
| `filter` | `/` | danh sách, trình duyệt repo |
| `add_account` | `a` | danh sách, chi tiết |
| `add_company` | `A` | danh sách |
| `edit` | `e` | danh sách, chi tiết |
| `delete` | `d` | danh sách, chi tiết |
| `clone` | `c` | trang chủ, danh sách, chi tiết, áp dụng (xong), kiểm tra |
| `check` | `k` | trang chủ, danh sách, chi tiết, áp dụng (xong), trình duyệt repo |
| `doctor` | `D` | trang chủ, danh sách, chi tiết |
| `apply` | `p` | trang chủ, danh sách, chi tiết, kết quả SSH key |
| `ssh_key` | `g` | danh sách, chi tiết |
| `import` | `i` | trang chủ, danh sách |
| `reload` | `r` | danh sách, trình duyệt repo (quét lại; tải lại ở tab Online) |
| `yes` / `no` | `y` / `n` | màn hình xem trước, hộp xác nhận (cả câu hỏi «bỏ những gì đã nhập?») |
| `fix` | `f` | kiểm tra, trình duyệt repo |
| `retry` | `r` | áp dụng, kiểm tra, chẩn đoán |
| `copy` | `c` | kết quả SSH key, trình duyệt repo (chép đường dẫn, hoặc URL clone ở tab Online) |
| `open` | `o` | kết quả SSH key, trình duyệt repo (tab Online: mở trang của repo) |
| `ssh_test` | `s` | chẩn đoán |
| `terminal` | `t` | clone |
| `problems` | `!` | trình duyệt repo (chỉ hiện repo có vấn đề) |
| `palette` | `ctrl+k` | trang chủ, mọi màn hình trừ form (mở cùng bảng với `help`) |

Một phím là một ký tự ASCII in được (`v`, `V`, `/`, `5`), `ctrl+<chữ>`, `alt+<ký tự>` hoặc `f1` đến `f12`. Không hợp lệ: chữ có dấu (`â`, `đ`), từ (`ab`) và phím có tên như `enter`, `esc`, `tab`, mũi tên hay `space`. Đặt phím trong dấu nháy khi YAML sẽ hiểu khác đi: `"?"`, `"#"`, `"&"`, `"*"`, `"!"`, `"|"`, `">"`, `"%"`, `"@"`, `` "`" ``.

Các phím sau không đổi được: `ctrl+c`, `esc` (luôn quay lại một bước), các số `1` đến `9` ở trang chủ, `enter`, `tab` và `shift+tab`, mũi tên, `pgup`/`pgdn`, `home`/`end`, `ctrl+p`/`ctrl+n`/`ctrl+b`/`ctrl+f`, `space`, `h` và `l` trong hộp hai nút, phím của ô chọn trong biểu mẫu (`y n h l j k`) và mọi chữ bạn gõ vào ô nhập. Trong các phím `ctrl+<chữ>`, `ctrl+h`, `ctrl+i` và `ctrl+m` (backspace, tab, enter ở nhiều terminal) cũng bị từ chối.

Hai hành động cùng hoạt động trên một màn hình không được dùng chung phím (ví dụ `check` và `clone` ở danh sách), nhưng một phím có thể có nghĩa khác ở những màn hình không bao giờ hiện cùng lúc. Trang chủ dùng chung các chữ với màn hình Tài khoản (`c`, `k`, `p`, `D`, `i` mở cùng các mục), và không gán được một chữ số cho hành động nào hoạt động ở trang chủ (`quit`, `help`, `palette`, `clone`, `check`, `apply`, `doctor`, `import`): phép gán đó bị báo và hành động giữ phím mặc định. Hoán đổi hai phím thì được: `clone: "e"` cùng `edit: "c"`. Một vấn đề không bao giờ làm gitid dừng: hành động bị ảnh hưởng giữ phím mặc định và gitid báo cho bạn, trên thanh trạng thái khi mở TUI, ở cuối bảng `?`, trong `gitid keys` (thoát với mã 2) và trong `gitid doctor` (cảnh báo). Tệp không đọc được hoặc sai cú pháp thì mọi phím về mặc định. Tên hành động không có (ví dụ từ gitid bản mới hơn) được báo và bỏ qua.

Bấm `r` ở danh sách để tải lại `config.yaml` và `keys.yaml` mà không thoát TUI. Phím tắt cần bàn phím gửi chữ thường thuần: khi bộ gõ tiếng Việt (Telex, VNI) đang bật, gitid không đọc được `a`, `k`… và báo trên thanh trạng thái; hãy chuyển sang EN (ABC) trước. Ngoại lệ duy nhất là ô lọc của bảng `?`, vì đó là ô nhập chữ: gõ tiếng Việt ở đó được. Vì lý do đó `keys.yaml` cũng không nhận phím có ký tự ngoài ASCII. Nếu một `keys.yaml` viết trước khi có `palette` gán `ctrl+k` cho hành động khác trong màn hình mà palette dùng chung, phím đó bị báo là xung đột và hành động quay về phím mặc định; hãy gán lại `palette` để giữ phím của bạn.

## Tính năng chi tiết

### Trang chủ

`gitid` mở một menu, không phải danh sách tài khoản. Nó hiện chữ gitid, câu giới thiệu kèm phiên bản, một dòng báo khi có bản mới, và tám việc, mỗi việc có số và một dòng mô tả:

```text
  1  Tài khoản      Công ty, tài khoản và SSH key
  2  Clone repo     Vào đúng thư mục, với đúng tài khoản
  3  Kiểm tra repo  Repo ở thư mục này dùng tài khoản nào
  4  Duyệt repo     Repo trên máy và trên các nền tảng
  5  Áp dụng        Xem trước rồi ghi vào ~/.gitconfig và ~/.ssh
  6  Chẩn đoán      Kiểm tra git, SSH và cấu hình
  7  Nhập từ git    Đọc includeIf có sẵn trong ~/.gitconfig
  8  Cài đặt        Ngôn ngữ giao diện
```

Bấm `1` đến `8` để mở ngay một việc, hoặc di chuyển con trỏ `▌` bằng `↑`/`↓` (cả `home`/`end` và `pgup`/`pgdn`; con trỏ dừng ở hai đầu) rồi bấm `enter`. Các chữ bạn quen dùng ở màn hình Tài khoản vẫn chạy từ trang chủ: `c` clone, `k` kiểm tra, `p` áp dụng, `D` chẩn đoán, `i` nhập từ git (Cài đặt không có chữ: dùng `8` hoặc `enter`). `q` thoát, còn `esc` không làm gì ở đây; ở mọi màn hình khác `esc` và `q` quay lại một bước, nên `esc` ở Tài khoản đưa về trang chủ (bộ lọc đang áp dụng được xóa trước). Chữ số chỉ là phím tắt ở trang chủ: trong biểu mẫu, ô lọc hay bảng `?` chúng được gõ như chữ. Sau khi áp dụng xong, `enter` quay về màn hình đã bắt đầu luồng đó (trang chủ, hoặc Tài khoản nếu bạn bắt đầu từ đó). Khi có bản mới, một dòng báo và nêu lệnh hợp với cách bạn cài gitid (`brew upgrade gitid` với Homebrew, `install.sh` với script cài, hoặc trang releases nếu không rõ); xem [Mạng](#an-toàn) để biết việc kiểm tra gửi gì và cách tắt.

### Cài đặt

`8` ở trang chủ mở Cài đặt. Hiện có một mục, ngôn ngữ giao diện, hiện dưới dạng lựa chọn: `◉ Tiếng Việt  ○ English`. `←`, `→` và `enter` chuyển sang ngôn ngữ còn lại. Thay đổi có hiệu lực ngay ở mọi màn hình (thanh trợ giúp, bảng `?`, trang chủ), và gitid lưu vào `settings.language` trong `config.yaml` ở nền, kèm bản sao lưu của tệp cũ như mọi lần lưu khác. Đây không phải thay đổi chờ áp dụng: `apply` không có gì để ghi cho nó. Nếu không đọc được `config.yaml` hoặc tệp không hợp lệ, ngôn ngữ vẫn đổi trong lần chạy này và thanh trạng thái báo không lưu được; tệp giữ nguyên. Nếu biến môi trường `GITID_LANG` đang được đặt, màn hình nói rõ: lựa chọn của bạn vẫn được lưu, nhưng lần mở sau gitid dùng lại biến đó. `esc` quay về trang chủ.

### Danh sách

Màn hình Tài khoản là cây công ty và tài khoản với thẻ nền tảng (`GH`, `GL`, `BB`, `GT`, `AZ`), kiểu xác thực, `★` cho tài khoản mặc định của công ty và email. Nhấn `/` để lọc theo tên, username, email, host hoặc nền tảng; việc so khớp là mờ trong từng từ và giữ nguyên thứ tự của cây. Từ 84 cột, khung chi tiết nằm cạnh danh sách; từ 60 cột (và 24 dòng) nó nằm dưới danh sách; terminal nhỏ hơn thì nhấn `enter` để mở. Khung này giải thích công ty hoặc tài khoản đang chọn sẽ làm gì ("Điều gì sẽ xảy ra"); với tài khoản, nó còn hiện remote được tạo ra, thư mục và bước tiếp theo. Hai tài khoản cùng công ty có cùng nền tảng, host và username được đánh dấu `!`. Công ty chưa có tài khoản được đánh dấu `chưa có tài khoản`: gitid chưa ghi gì cho nó cho tới khi bạn thêm tài khoản bằng `a`.

Cuối mỗi màn hình có thanh trợ giúp hai dòng (một dòng khi terminal dưới 14 dòng). Dòng 1 là phím của dòng đang chọn hoặc của bước hiện tại, `▸` đánh dấu bước nên làm tiếp; dòng 2 là các phím luôn dùng được (ở danh sách: `↑↓` di chuyển, `A` thêm công ty, `a` thêm tài khoản), rồi `? tất cả phím`, và phím thoát (`q` thoát ở trang chủ, `esc` quay lại ở nơi khác) đứng cuối. Nhãn đứng trước mỗi dòng ("Dòng này:", "Luôn có:") chỉ hiện khi mọi phím của dòng đều vừa chỗ cùng nhãn. Khung danh sách có viền đậm, cho biết phím mũi tên đang tác động vào đâu; khung chi tiết bên cạnh hoặc bên dưới có viền mảnh. Nhấn `?` hoặc `ctrl+k` để mở bảng phím tắt: bảng này tìm kiếm được. Gõ để lọc theo mô tả (không phân biệt dấu và chữ hoa: `cong ty` tìm ra "thêm công ty") hoặc theo phím, di chuyển bằng `↑`/`↓`, nhấn `enter` để chạy dòng đang chọn, đúng như khi bạn bấm phím của nó. `esc` xóa bộ lọc rồi mới đóng; `?` và `ctrl+k` đóng bất cứ lúc nào; `q` đóng khi ô lọc còn trống. Dòng chỉ để di chuyển (`↑/↓`, cuộn trang) và các dòng "mọi màn hình" hiện mờ và không chạy được.

```text
▌ acme  ACME Corp                 ~/Code/acme/  2 tài khoản
 ├─ ★ alice      GH https                  alice@example.com
 └─   carol      GH ssh                    carol@example.com
▪ globex  Globex                ~/Code/globex/  1 tài khoản
 └─ ★ dave       GL ssh                  dave@globex.example

                          ● 6 thay đổi chưa áp dụng — bấm p
 ▸ p xem trước & áp dụng  a thêm tài khoản  e sửa  d xóa  esc quay lại  ? tất cả phím
```

Bên phải thanh trạng thái luôn cho biết còn việc gì: thay đổi chưa áp dụng (được tô nổi), công ty chưa có tài khoản, hoặc `✓ Đã áp dụng, không còn thay đổi`. Thanh phím chỉ hiện các phím đang cần; bước tiếp theo đứng đầu và có dấu `▸`, còn `?` liệt kê mọi phím.

`q` thoát ở trang chủ và quay lại một bước ở mọi màn hình khác, giống `esc` (trong ô nhập chữ, `q` được gõ như một chữ cái). `r` tải lại `config.yaml` và `keys.yaml` từ đĩa, ví dụ sau khi bạn sửa tay. Ở màn hình chào, `enter` thêm công ty đầu tiên. Mọi phím ở trên đều đổi được, xem [Tùy chỉnh phím tắt](#tùy-chỉnh-phím-tắt).

### Thêm và sửa tài khoản

`A` thêm công ty, `a` thêm tài khoản vào công ty đang chọn, `e` sửa, `d` xóa. Biểu mẫu kiểm tra dữ liệu ngay khi nhập, theo đúng các luật mà CLI dùng. Nếu chọn SSH, gitid đề nghị tạo key ngay sau khi lưu. Sau mỗi thay đổi bạn xem diff rồi xác nhận.

### Clone và trình duyệt repo

`c` mở trình duyệt repo. Nó có ba tab, đổi bằng `tab` và `shift+tab`: **Trên máy**, **URL** và **Online**. Nó mở ở tab **URL**, nên dán URL rồi bấm `enter` vẫn hoạt động y như trước.

**Tab URL.** gitid hỏi URL, xếp hạng các tài khoản (tài khoản cùng host lên trước), xem trước URL đã viết lại và thư mục đích, rồi clone ở chế độ nền với thanh tiến trình. Xong, gitid kiểm tra email mà git sẽ dùng trong repo mới. Không đổi tab được khi đang clone.

```text
URL gốc:   https://github.com/org/repo.git
URL clone:  https://alice@github.com/org/repo.git
Thư mục:    ~/Code/acme/repo
✓ Đã clone xong
✓ user.email trong repo: alice@example.com (đúng).
```

Nếu xác thực thất bại ở chế độ nền, gitid đề nghị chạy lại ngay trên terminal, nơi bạn nhập được mật khẩu hoặc passphrase.

**Tab Trên máy.** Khi trình duyệt mở, gitid quét thư mục gốc của từng công ty ở chế độ nền và liệt kê các repo tìm được, theo công ty, kèm tài khoản mà remote khớp và điều gì đang sai. Nhãn tab hiện tổng số (`Trên máy 42`; `Trên máy …` khi đang quét).

```text
◂ Tất cả công ty ▸  7 repo · 5 vấn đề
acme  ~/Code/acme/  5 repo · 3 vấn đề
▌✓ api            acme/alice
 ! org/x          acme/alice   sai username/alias
 ! bb                          host lạ
 ! dup            acme/alice   ×2
globex  ~/Code/globex/  0 repo · 0 vấn đề
  ! ~/Code/globex/ chưa tồn tại. Clone một repo để tạo thư mục này.
```

| Phím | Tác dụng |
| ---- | -------- |
| `k` hoặc `enter` | kiểm tra repo đang chọn (màn hình Kiểm tra bên dưới) |
| `f` | kiểm tra rồi hỏi sửa remote ngay; chỉ hiện khi có gì để sửa |
| `c` | chép đường dẫn tuyệt đối (nếu chép lỗi, đường dẫn hiện ở thanh trạng thái) |
| `/` | lọc theo đường dẫn, tài khoản hoặc công ty |
| `!` | chỉ hiện repo có vấn đề |
| `←` `→` | chuyển giữa tất cả công ty và từng công ty |
| `r` | quét lại |
| `esc` | dừng lần quét đang chạy; `esc` lần hai rời màn hình |

Mỗi dòng có một dấu (`✓` ổn, `!` cần chú ý, `✗` git không đọc được, `·` repo bare), đường dẫn bên dưới thư mục công ty, tài khoản và một ghi chú ngắn. Trạng thái có thể là: sai username/alias (tài khoản khớp nhưng remote không đúng dạng của nó), tài khoản lạ (host đã biết nhưng không có tài khoản nào có username đó), host lạ, không có remote, remote không đọc được, chưa rõ tài khoản, lỗi git và chủ sở hữu lạ (git từ chối thư mục vì `safe.directory`). Cờ: email đang dùng khác email của tài khoản, `user.email` được đặt trong `.git/config` của chính repo, repo nằm trong thư mục của công ty khác, cùng một remote được clone nhiều lần (`×2`; worktree không tính) và symlink trỏ tới repo. Worktree, submodule và repo bare được ghi rõ. Khối dưới danh sách cho biết chi tiết của repo đang chọn và, khi có thể sửa, remote sẽ được đặt thành gì.

Cách quét: tìm sâu tối đa 4 thư mục bên dưới mỗi thư mục công ty, thăm tối đa 20 000 thư mục mỗi công ty (quá thì báo và hiện những gì đã tìm được), và bỏ qua `node_modules`, `vendor` cùng các thư mục ẩn. Nó không bao giờ đi vào trong một repo và không theo symlink (symlink trỏ thẳng tới một repo thì được liệt kê và ghi là symlink). Thư mục không đọc được sẽ được báo cùng ba đường dẫn đầu tiên. Mỗi repo một lệnh `git config`, chạy 8 repo cùng lúc, để đọc remote và email mà git sẽ dùng. Kết quả được giữ đến khi bạn thoát gitid: mở lại trình duyệt là thấy ngay, `r` quét lại, cấu hình đổi thì áp dụng lên cùng kết quả mà không quét lại, và một lần clone hoặc sửa xong chỉ cập nhật đúng repo đó. Rời màn hình sẽ dừng lần quét đang chạy.

Công ty chưa có thư mục sẽ được báo («chưa tồn tại, clone một repo để tạo»); chưa có công ty nào thì tab nhắc bạn thêm công ty ở danh sách chính trước.

**Tab Online.** Liệt kê repo mà từng tài khoản trong cấu hình thấy được trên nền tảng của nó (GitHub và GitHub Enterprise Server, GitLab và GitLab tự dựng, Gitea, Bitbucket Cloud và Azure DevOps Services). Chỉ tài khoản có token mới được tải, và chưa có gì được tìm hay gửi đi cho đến khi bạn mở tab này. `←` `→` đổi tài khoản; thanh trên cùng hiện tài khoản, nền tảng và nguồn token (`token: env`, `keychain`, `gh`, `glab`, `git` hoặc `none`). Bản thân token không bao giờ được hiện.

```text
◂ acme/alice · GitHub · token: gh ▸  (1/5)
  Token của @alice · 5 repo
▌✓ acme/api         riêng 3d  +2 tk  ~/Code/acme/api
 ! acme/x                  3d  +1 tk  ~/Code/acme/org/x
   acme-org/tools   rỗng   3d  +1 tk
```

Mỗi dòng hiện `chủ/tên`, nhãn (`riêng`, `lưu`, `fork`, `rỗng`), cập nhật cách đây bao lâu, `+N tk` khi tài khoản khác của bạn cũng thấy repo đó, và nếu bạn đã có repo này trên máy thì ở đâu (`✓` ổn, `!` bản trên máy có vấn đề). Khối dưới danh sách cho biết URL clone theo tài khoản và thư mục đích.

| Phím | Tác dụng |
| ---- | -------- |
| `enter` | chưa clone: mở luồng clone ở bước chọn tài khoản, đã chọn sẵn tài khoản này; đã clone: mở màn hình Kiểm tra cho bản đang có |
| `c` | chép URL clone (theo tài khoản, không có token) |
| `o` | mở trang web của repo trong trình duyệt |
| `/` | lọc theo `chủ/tên` |
| `←` `→` | đổi tài khoản |
| `r` | quên token của tài khoản này, tìm lại và tải lại |
| `esc` | dừng lần tải đang chạy (các trang đã tải vẫn còn); `esc` lần hai thì rời màn hình |

URL clone được dựng từ host trong cấu hình của bạn và đường dẫn repo, rồi viết lại như mọi URL bạn dán, nên dùng username hoặc ssh alias của tài khoản. Token của API không bao giờ dùng để clone: git đăng nhập bằng thông tin riêng của nó như mọi khi. Nếu `<thư mục gốc>/<repo>` đã tồn tại và là thứ khác, gitid clone vào `<thư mục gốc>/<chủ>-<repo>` và báo cho bạn; nếu thư mục đó cũng có rồi thì `enter` không làm gì và khối dưới danh sách nhắc dùng tab URL với thư mục khác. Repo online được ghép với bản clone trên máy theo `host/chủ/repo` (repo đổi tên hay chuyển chủ thì không theo được). Trước khi clone, gitid kiểm tra `config.yaml` chưa đổi kể từ lúc màn hình nạp nó; nếu đã đổi, nó nạp lại và nhắc bạn kiểm tra lại tài khoản.

#### `gitid repos`

`gitid repos` in cùng kết quả quét như tab Trên máy, nhóm theo công ty (mỗi repo một dòng, dấu `!` ở đầu dòng của repo cần xử lý, mỗi dòng rộng tối đa 80 cột). `--company acme` chỉ lấy một công ty, `--depth N` (từ 1 trở lên, mặc định 4) chỉ tìm N cấp thư mục dưới thư mục gốc của công ty, `--problems` chỉ lấy repo cần xử lý. `--json` in lược đồ ổn định bên dưới. Mã thoát: `0` đã liệt kê, không có gì sai; `2` có gì cần xử lý (vấn đề ở máy, hoặc một tài khoản online bị lỗi); `1` lỗi (cờ sai, cấu hình không hợp lệ, công ty hoặc tài khoản không có, hoặc Ctrl-C, sau khi in phần đã có).

`gitid repos --online` liệt kê những gì mỗi tài khoản thấy, dùng đúng chuỗi tìm token của tab Online (không bao giờ in token). Tài khoản chưa có token hiện `token: không có` kèm gợi ý; đó không phải thất bại (mã thoát `0`). `--account acme/alice` chỉ liệt kê một tài khoản (và chỉ tìm token của nó), `--max-pages N` (1 đến 500) giới hạn số trang mỗi tài khoản (khi đó `truncated` là true), `--local` thêm kết quả quét trên máy vào đầu ra (lúc nào cũng quét để điền `local_paths`).

```json
{"local": {"companies": [{"slug": "acme", "base_dir": "~/Code/acme/", "state": "ok", "truncated": false, "denied": 0,
   "repos": [{"path": "/abs/path", "kind": "repo", "remote": "...", "key": "github.com/acme/api", "account": "acme/alice",
              "reason": "username", "effective_email": "...", "email_origin": "...", "expected_email": "...",
              "status": "ok", "flags": [], "belongs_to": "", "duplicates": [], "fix_to": ""}]}]},
 "online": {"accounts": [{"account": "acme/alice", "platform": "github", "host": "github.com", "token_source": "gh",
   "login": "alice", "state": "ok", "error": "", "error_kind": "", "truncated": false, "warnings": [],
   "repos": [{"full_name": "acme/api", "owner": "acme", "name": "api", "private": true, "archived": false, "fork": false,
              "empty": false, "updated_at": "2026-11-10T10:00:00Z", "web_url": "...", "clone_url": "...", "local_paths": []}]}]}}
```

`state` của tài khoản online là `ok`, `no_token`, `error` hoặc `cancelled`; `error` là thông báo theo ngôn ngữ đang dùng, `error_kind` là mã ổn định. `local` bị bỏ khi có `--online` trừ khi thêm `--local`; `online` bị bỏ khi không có `--online`.

#### Tab Online và token

Với mỗi tài khoản, gitid tìm token theo thứ tự này và dừng ở nguồn đầu tiên có:

1. **Biến môi trường** `GITID_TOKEN_<CÔNG-TY>__<TÀI-KHOẢN>`: viết hoa, `-` thành `_`, hai dấu gạch dưới giữa hai phần. Với `acme/alice` là `GITID_TOKEN_ACME__ALICE`. Nếu hai tài khoản cho cùng một tên, biến bị bỏ qua và tab báo như vậy. gitid không đọc `GITHUB_TOKEN`, `GH_TOKEN` hay `GITLAB_TOKEN` vì chúng không phân biệt được tài khoản. Riêng chương trình `gh` có thể tự đọc chúng khi gitid nhờ nó lấy token (nguồn 3), nên hãy bỏ đặt các biến này nếu bạn muốn dùng token của chính đăng nhập `gh` của tài khoản. gitid cũng gỡ mọi biến `GITID_TOKEN_*` khỏi môi trường của các chương trình nó chạy.
2. **Mục Keychain của gitid** (macOS): service `gitid`, account `<host>/<username>` (ví dụ `github.com/alice`), đọc bằng `security find-generic-password -w`. Tạo nó bằng `gitid token set <công-ty>/<tài-khoản>` (bên dưới) hoặc tự dùng `security`.
3. **Đăng nhập `gh` hoặc `glab`:** `gh auth token --hostname=<host> --user=<username>` (`gh` cũ không có `--user` thì được hỏi lại không có cờ đó), hoặc `glab config get token --host=<host>`. `glab` chỉ giữ một token cho mỗi host.
4. **Credential helper của git:** `git credential fill` cho `https://<host>` và username của bạn, tức là bí mật git vẫn dùng để push. gitid chỉ chạy nó khi bạn mở tab Online, với `GIT_TERMINAL_PROMPT=0`, không chương trình askpass và `credential.interactive` đặt là `never` và `false` (hai giá trị Git Credential Manager ghi là «không hỏi»), nên nó không thể hỏi bạn gì; nó không bao giờ chạy `approve` hay `reject`, nên không đổi gì trong nơi helper lưu. Lần đọc đầu tiên vẫn có thể làm macOS hiện hộp thoại «cho phép truy cập Keychain» của chính nó, gitid không tắt được. Thứ helper lưu có thể là mật khẩu chứ không phải token; khi đó GitHub trả 401 và tab báo thông tin đã lưu giống một mật khẩu.

Token được giữ trong bộ nhớ suốt phiên (nên hộp thoại Keychain không lặp lại) và gitid không bao giờ ghi nó ra đĩa. Nó không bao giờ nằm trong dòng lệnh, URL, log hay thông báo. Nguồn nào không có hoặc lỗi thì thử nguồn kế tiếp; sau khi một lần gọi API thất bại thì không có phương án dự phòng, và lỗi nêu tên nguồn để bạn biết sửa bí mật nào (`r` tìm lại).

**Lưu token.** `gitid token set acme/alice` đọc token từ stdin: dùng đường ống (`pbpaste | gitid token set acme/alice`) hoặc gõ khi được hỏi (không hiện ký tự). Chỉ nhận ASCII in được, không có khoảng trắng, dài 1 đến 4096 ký tự. gitid chạy `security -i` và gửi cho nó `add-generic-password -U -s gitid -a "<host>/<username>" -X <hex của token>` qua **stdin**, nên token không nằm trong dòng lệnh nào (`security add-generic-password -w <token>` sẽ hiện nó trong `ps`, còn `-w` đặt cuối thì hỏi trên terminal chứ không đọc từ ống). Mục đã có sẽ được thay. `gitid token rm acme/alice` xóa mục (không có gì để xóa không phải là lỗi). `gitid token status` cho biết với từng tài khoản nguồn nào sẽ được dùng (`env`, `keychain`, `gh`, `glab`, `git` hoặc `none`) và tên biến cần đặt; nó không bao giờ in token. Vì chạy cùng cách tìm như tab Online, credential helper của git có thể hiện hộp thoại Keychain riêng của nó. Trên hệ điều hành khác, `token set` và `token rm` báo không hỗ trợ và nêu tên biến cần đặt thay thế.

Trước khi liệt kê gì, gitid hỏi API token thuộc về ai (`GET /user`). Nếu login đó khác username của tài khoản (không phân biệt hoa thường) thì không liệt kê gì và tab báo «token này thuộc về @x». Quyền cần có: token classic của GitHub cần `repo` (hoặc `public_repo` nếu chỉ repo công khai), token fine-grained cần «Metadata: read»; GitLab cần `read_api`; Gitea cần `read:repository` và `read:organization`; API token của Bitbucket Cloud cần `read:repository:bitbucket` và `read:workspace:bitbucket` (chưa xác nhận được: Atlassian không liệt kê quyền của các lệnh gọi này); PAT của Azure DevOps cần `Code (Read)`. gitid không bao giờ yêu cầu hay tạo token.

Theo từng nền tảng: **Gitea** (host của bạn, `https://<host>/api/v1`) gửi `Authorization: token <t>` và liệt kê `/user/repos` cùng `/orgs/<org>/repos` cho từng mục của `/user/orgs`. **Bitbucket Cloud** (chỉ `bitbucket.org`, `https://api.bitbucket.org/2.0`) dùng API token của Atlassian (app password đã ngừng hoạt động từ 9/6/2026): nếu tài khoản có `email` thì thử Basic với email, rồi header Bearer; nếu không có thì thử header Bearer, rồi Basic với username của tài khoản; liệt kê `/user/workspaces` và `/repositories/<workspace>?role=member` cho từng workspace. `username` của tài khoản phải khớp nickname trên Bitbucket. **Azure DevOps Services** (chỉ `dev.azure.com`) dùng PAT với Basic và tên người dùng để trống (hoặc token Microsoft Entra dạng Bearer) và liệt kê `https://dev.azure.com/<org>/_apis/git/repositories?api-version=7.1` cho từng organization trong `orgs` của tài khoản; nếu không có `orgs`, gitid thử tự tìm (`app.vssps.visualstudio.com`), nơi Microsoft có thể từ chối PAT, rồi báo hãy đặt `orgs`. Azure không cho biết PAT thuộc về ai hay repo đổi lần cuối khi nào nên hai thông tin đó không được hiện.

Yêu cầu chỉ là `GET`, tới `https://api.github.com` (GitHub.com), `https://<host>/api/v3` (GitHub Enterprise Server) hoặc `https://<host>/api/v4` (GitLab), mỗi yêu cầu tối đa 30 giây, tối đa 3 tài khoản cùng lúc, tối đa 50 trang 100 repo mỗi tài khoản (Gitea: 50 mỗi trang, tối đa 100 organization hoặc workspace; quá thì tab báo đã dừng) và tối đa 8 MiB mỗi trang. Chuyển hướng hoặc liên kết «trang kế» tới host khác, hoặc tới `http` thường, đều bị từ chối, nên token không bao giờ đi đâu ngoài host của chính tài khoản. `HTTPS_PROXY` và `NO_PROXY` được tôn trọng; chứng chỉ được kiểm tra theo gốc tin cậy của hệ thống cộng với tệp `SSL_CERT_FILE` nếu có (không bao giờ tắt kiểm tra). Giới hạn truy cập ngắn hơn 10 giây thì được chờ một lần; dài hơn thì báo kèm giờ được đặt lại và các trang đã tải vẫn còn. Chưa hỗ trợ: GitHub Enterprise có tiền tố đường dẫn, GitHub Enterprise Cloud lưu dữ liệu theo vùng (`*.ghe.com`), GitLab dưới relative URL root, Bitbucket Data Center và Azure DevOps Server (tài khoản ở host khác `bitbucket.org` hay `dev.azure.com` được báo là chưa hỗ trợ: không gửi yêu cầu nào và không tính là lỗi).

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

`D` chạy mọi kiểm tra cùng lúc, mỗi mục một spinner riêng: phiên bản git, cấu hình hợp lệ, credential helper, `Include` của ssh, `gitid.conf`, key và quyền truy cập, thay đổi chưa áp dụng, thư mục gốc, trùng alias và pattern mơ hồ. Nhấn `s` để thử thêm `ssh -T` tới từng alias (một trong ba việc dùng mạng, và chỉ khi bạn nhấn).

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

gitid cần tối thiểu 40x10 ô và đẹp nhất từ 80x24; khung chi tiết hiện từ 84 cột (cạnh danh sách) hoặc từ 60x24 (dưới danh sách). Nếu màn hình bị rối sau khi chương trình sập, chạy `reset`. TUI chỉ chạy trên terminal thật; trong script hãy dùng các lệnh con.

</details>

## Gỡ cài đặt chương trình

Phần này chỉ gỡ chương trình `gitid`. Nó khác với [gỡ cấu hình do gitid quản lý](#an-toàn) (mục **Gỡ bỏ** ở đó); hãy làm việc đó trước nếu bạn muốn tệp git và ssh trở về đúng như ban đầu.

- **Homebrew:** `brew uninstall --cask gitid` (có thể thêm `brew untap tinhnd1902/tap`).
- **Script cài đặt:** `rm "$(command -v gitid)"`, rồi xóa dòng có chú thích `# added by gitid installer` khỏi tệp khởi động của shell (`~/.zshrc`, `~/.bash_profile`, `~/.bashrc` hoặc `~/.profile`), hoặc với fish thì xóa `~/.config/fish/conf.d/gitid.fish`.
- **`go install`:** `rm "$(go env GOPATH)/bin/gitid"`.

## Đóng góp

Rất hoan nghênh đóng góp: fork repo, tạo nhánh và mở pull request. Xem [CONTRIBUTING.md](CONTRIBUTING.md) để biết các lệnh make và quy tắc kiểm thử (test không bao giờ chạm vào HOME thật của bạn). Vui lòng đọc [Quy tắc ứng xử](CODE_OF_CONDUCT.md), và báo lỗ hổng bảo mật theo cách riêng tư như trong [SECURITY.md](SECURITY.md).

## Giấy phép

[MIT](LICENSE) © 2026 tinhnd1902
