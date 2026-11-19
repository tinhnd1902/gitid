package i18n

// viCLIRepos holds the Vietnamese strings of `gitid repos` and `gitid
// token`, and the Online strings the Gitea, Bitbucket and Azure DevOps
// listings add.
var viCLIRepos = map[string]string{
	// gitid repos
	"cli.repos.short":         "Liệt kê repository của các công ty (bản clone trên máy, hoặc trên mạng với --online)",
	"cli.flag.company":        "chỉ công ty này (slug)",
	"cli.flag.depth":          "tìm repository sâu tối đa bao nhiêu cấp dưới thư mục công ty (mặc định 4)",
	"cli.flag.problems":       "chỉ hiện repository cần xử lý",
	"cli.flag.online":         "liệt kê những gì mỗi tài khoản thấy trên nền tảng (cần token)",
	"cli.flag.local":          "cùng với --online, liệt kê cả bản clone trên máy",
	"cli.flag.max_pages":      "số trang tối đa cho mỗi tài khoản (mặc định 50)",
	"cli.repos.depth_invalid": "--depth phải từ 1 trở lên.",
	"cli.repos.pages_invalid": "--max-pages phải nằm trong khoảng 1 đến 500.",
	"cli.repos.company_none":  "Cấu hình không có công ty %q.",
	"cli.repos.cancelled":     "Đã hủy. Kết quả ở trên chưa đầy đủ.",
	"cli.repos.token":         "token: %s",
	"cli.repos.token_none":    "token: không có",
	"cli.repos.login":         "@%s",
	"cli.repos.unsupported":   "chưa hỗ trợ tài khoản %s",

	// gitid token
	"cli.token.short":         "Lưu, xóa hoặc xem token API của một tài khoản (Keychain của macOS)",
	"cli.token.set.short":     "Lưu token đọc từ stdin vào Keychain của macOS",
	"cli.token.rm.short":      "Xóa mục Keychain của gitid cho một tài khoản",
	"cli.token.status.short":  "Cho biết token của từng tài khoản lấy từ đâu (không bao giờ in token)",
	"cli.token.prompt":        "Token của %s (không hiện khi gõ): ",
	"cli.token.saved":         "Đã lưu token của %s vào Keychain của macOS (service gitid).",
	"cli.token.removed":       "Đã xóa mục Keychain của %s.",
	"cli.token.nothing":       "Không có gì để xóa cho %s.",
	"cli.token.unsupported":   "Chỉ lưu được token trên macOS. Hãy đặt biến %s.",
	"cli.token.bad":           "Token phải gồm 1 đến 4096 ký tự ASCII in được, không có khoảng trắng.",
	"cli.token.empty":         "Chưa có token. Hãy đưa vào qua stdin, hoặc gõ khi được hỏi.",
	"cli.token.failed":        "Lệnh security thất bại; chưa lưu gì cả.",
	"cli.token.rm_failed":     "Lệnh security thất bại; chưa xóa được gì.",
	"cli.token.bad_account":   "Host hoặc username của tài khoản này không lưu được vào Keychain.",
	"cli.token.no_secret_tty": "Không đọc được token ẩn ở đây. Hãy đưa token vào qua stdin.",
	"cli.token.status.none":   "không có (đặt %s, hoặc chạy: gitid token set %s)",
	"cli.token.status.pw":     "%s (giống mật khẩu, không phải token)",

	// Online: Gitea, Bitbucket, Azure DevOps
	"tui.repos.online.no_token.gitea":       "Chưa có token. Đặt biến %[1]s, hoặc chạy: gitid token set %[2]s",
	"tui.repos.online.no_token.bitbucket":   "Chưa có token. Đặt biến %[1]s, hoặc chạy: gitid token set %[2]s",
	"tui.repos.online.no_token.azure":       "Chưa có token. Đặt biến %[1]s là một PAT, hoặc chạy: gitid token set %[2]s",
	"tui.repos.online.hint.scope.gitea":     "Cấp cho token quyền read:repository và read:organization.",
	"tui.repos.online.hint.scope.bitbucket": "Cấp cho API token quyền read:repository:bitbucket và read:workspace:bitbucket.",
	"tui.repos.online.hint.scope.azure":     "Cấp cho PAT quyền Code (Read) của organization.",
	"tui.repos.online.hint.azure_signin":    "Tạo PAT mới cho organization với quyền Code (Read).",
	"tui.repos.online.hint.azure_orgs":      "Azure không liệt kê được organization của bạn bằng token này. Hãy thêm chúng vào orgs của tài khoản.",
	"tui.repos.online.warn.no_orgs":         "Không thấy organization Azure DevOps nào. Hãy thêm chúng vào orgs của tài khoản.",
	"tui.repos.online.warn.org_failed":      "Không liệt kê được organization %s.",
	"tui.repos.online.warn.ws_failed":       "Không liệt kê được workspace %s.",
	"tui.repos.online.warn.orgs_failed":     "Không liệt kê được các organization của người dùng này; repo của chúng sẽ thiếu.",
	"tui.repos.online.done_nologin":         "%s repo",
	"tui.repos.online.empty_nologin":        "Token này chưa thấy repo nào.",
}

func init() {
	for k, v := range viCLIRepos {
		vi[k] = v
	}
}
