package i18n

// viOnline holds the Vietnamese strings of the Online tab of the repo
// browser.
var viOnline = map[string]string{
	"tui.key.refetch":    "tải lại",
	"tui.key.copy_url":   "chép URL",
	"tui.key.open_page":  "mở trang",
	"tui.key.account":    "đổi tài khoản",
	"tui.key.stop_fetch": "dừng tải",

	// account bar and states
	"tui.repos.online.token":         "token: %s",
	"tui.repos.online.no_accounts":   "Chưa có tài khoản nào. Hãy thêm tài khoản ở danh sách chính trước.",
	"tui.repos.online.unsupported":   "Chưa liệt kê được repository của tài khoản %s.",
	"tui.repos.online.resolving":     "Đang tìm token…",
	"tui.repos.online.fetching":      "Đang tải %s · trang %d · %s repo",
	"tui.repos.online.done":          "Token của @%s · %s repo",
	"tui.repos.online.cancelled":     "Đã dừng tải. Bấm %[1]s để thử lại.",
	"tui.repos.online.truncated":     "Dừng sau %d trang; đang hiện %s repo.",
	"tui.repos.online.empty":         "@%s chưa thấy repo nào.",
	"tui.repos.online.no_match":      "Không có gì khớp “%s”.",
	"tui.repos.online.clear_filter":  "(esc: xóa bộ lọc)",
	"tui.repos.online.none_selected": "Chọn một repository để xem chi tiết.",

	// no token
	"tui.repos.online.no_token.github": "Chưa có token. Chạy gh auth login, push một lần qua HTTPS, hoặc đặt biến %s.",
	"tui.repos.online.no_token.gitlab": "Chưa có token. Chạy glab auth login, push một lần qua HTTPS, hoặc đặt biến %s.",
	"tui.repos.online.env_ambiguous":   "Một tài khoản khác cũng dùng biến %s nên biến bị bỏ qua. Hãy đổi tên tài khoản hoặc công ty.",
	"tui.repos.online.invalid_value":   "Host hoặc username của tài khoản này không dùng được để tìm token.",

	// warnings of a listing that worked
	"tui.repos.online.warn.no_repo_scope": "Token không có scope repo: repo private bị ẩn.",
	"tui.repos.online.warn.sso_partial":   "Một số tổ chức bị ẩn cho đến khi token được cấp quyền SSO.",

	// errors
	"tui.repos.online.err.auth":         "Token bị từ chối: sai hoặc đã hết hạn.",
	"tui.repos.online.err.scope":        "Token không được phép liệt kê repository: thiếu scope hoặc quyền.",
	"tui.repos.online.err.sso":          "Tổ chức yêu cầu đăng nhập một lần SAML cho token này.",
	"tui.repos.online.err.otp":          "Thông tin đăng nhập đã lưu là mật khẩu cần mã dùng một lần, không phải token.",
	"tui.repos.online.err.rate_limit":   "Đã chạm giới hạn truy cập. Giới hạn được đặt lại lúc %s.",
	"tui.repos.online.err.rate_unknown": "Đã chạm giới hạn truy cập. Hãy thử lại sau.",
	"tui.repos.online.err.network":      "Không kết nối được tới máy chủ.",
	"tui.repos.online.err.dns":          "Không phân giải được tên máy chủ. Bạn đang offline?",
	"tui.repos.online.err.timeout":      "Máy chủ không trả lời kịp.",
	"tui.repos.online.err.tls":          "Chứng chỉ của máy chủ không được tin cậy.",
	"tui.repos.online.err.proxy":        "Không kết nối được tới proxy.",
	"tui.repos.online.err.server":       "Máy chủ bị lỗi (HTTP %d).",
	"tui.repos.online.err.bad_response": "Câu trả lời không phải của API (trang HTML, máy chủ khác, hoặc quá lớn).",
	"tui.repos.online.err.wrong_user":   "Token này thuộc về @%s, không phải tài khoản này.",
	"tui.repos.online.err.insecure":     "Host của tài khoản này không phải host https thường.",
	"tui.repos.online.err.cancelled":    "Đã hủy.",
	"tui.repos.online.err.other":        "Không liệt kê được repository.",

	// hints
	"tui.repos.online.hint.auth.env":          "Kiểm tra giá trị của biến %s.",
	"tui.repos.online.hint.auth.keychain":     "Thay mục Keychain của gitid (service gitid, account %s).",
	"tui.repos.online.hint.auth.gh":           "Chạy gh auth login để đăng nhập lại.",
	"tui.repos.online.hint.auth.glab":         "Chạy glab auth login để đăng nhập lại.",
	"tui.repos.online.hint.auth.git":          "git từ chối thông tin đã lưu. Đăng nhập bằng gh, hoặc đặt %s thành personal access token.",
	"tui.repos.online.hint.auth.git_password": "Thông tin đã lưu giống một mật khẩu. Hãy tạo personal access token và đặt %s, hoặc đăng nhập bằng gh.",
	"tui.repos.online.hint.wrong_user":        "Hãy đăng nhập đúng tài khoản này (gh auth login, glab auth login) hoặc đặt %s.",
	"tui.repos.online.hint.scope.github":      "Cấp cho token scope repo (classic) hoặc Metadata: read (fine-grained).",
	"tui.repos.online.hint.scope.gitlab":      "Cấp cho token scope read_api.",
	"tui.repos.online.hint.sso":               "Cấp quyền cho token ở tổ chức: %s",
	"tui.repos.online.hint.sso_nourl":         "Cấp quyền cho token ở tổ chức trong phần cài đặt token.",
	"tui.repos.online.hint.tls":               "Cài CA vào Keychain, hoặc đặt SSL_CERT_FILE trỏ tới file PEM.",
	"tui.repos.online.hint.proxy":             "Kiểm tra HTTPS_PROXY và NO_PROXY.",
	"tui.repos.online.hint.retry":             "Kiểm tra kết nối rồi bấm %[1]s.",
	"tui.repos.online.hint.otp":               "Hãy tạo personal access token và đặt %s.",
	"tui.repos.online.hint.bad_response":      "Kiểm tra host của tài khoản trong cấu hình.",

	// rows and details
	"tui.repos.online.badge.private":  "riêng",
	"tui.repos.online.badge.archived": "lưu",
	"tui.repos.online.badge.fork":     "fork",
	"tui.repos.online.badge.empty":    "rỗng",
	"tui.repos.online.also":           "+%d tk",
	"tui.repos.online.d.cloned":       "Đã clone ở",
	"tui.repos.online.d.clone_hint":   "Bấm enter để clone.",
	"tui.repos.online.d.check_hint":   "Đã clone rồi. Bấm enter để kiểm tra.",
	"tui.repos.online.d.problem_hint": "Bản clone trên máy có vấn đề. Bấm enter để kiểm tra.",
	"tui.repos.online.d.unclonable":   "gitid không clone được tên này.",
	"tui.repos.online.d.taken":        "Các thư mục đích đã tồn tại (thử sau cùng: %s). Hãy dùng tab URL với thư mục khác.",
	"tui.repos.online.stale":          "config.yaml đã đổi — đã nạp lại, hãy kiểm tra lại tài khoản.",
	"tui.repos.online.copied":         "Đã chép: %s",
	"tui.repos.online.copy_failed":    "Không chép được. URL là: %s",
	"tui.repos.online.opened":         "Đã mở: %s",
	"tui.repos.online.open_failed":    "Không mở được trang: %s",

	// notes of a clone (internal/app)
	"online.warn.dir_fallback":  "Thư mục đó đã có; sẽ clone vào %s.",
	"online.warn.other_account": "git sẽ dùng %s cho URL này.",
	"online.warn.no_account":    "Không tài khoản nào khớp URL này (kiểm tra orgs của tài khoản).",
}

func init() {
	for k, v := range viOnline {
		vi[k] = v
	}
}
