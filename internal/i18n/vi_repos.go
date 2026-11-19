package i18n

// viRepos holds the Vietnamese strings of the repo browser (the local scan
// and the Repo screen).
var viRepos = map[string]string{
	"tui.key.problems": "chỉ repo có vấn đề",

	// repo browser: the Repo screen with its Local · URL · Online tabs
	"tui.repos.title":      "Repo",
	"tui.repos.tab.local":  "Trên máy",
	"tui.repos.tab.url":    "URL",
	"tui.repos.tab.online": "Online",
	"tui.key.tab":          "đổi tab",
	"tui.key.group":        "đổi công ty",
	"tui.key.copy_path":    "sao chép đường dẫn",
	"tui.key.rescan":       "quét lại",
	"tui.key.stop_scan":    "dừng quét",

	// Local tab
	"tui.repos.local.all":               "Tất cả công ty",
	"tui.repos.local.repos_one":         "1 repo",
	"tui.repos.local.repos_many":        "%d repo",
	"tui.repos.local.problems_one":      "1 vấn đề",
	"tui.repos.local.problems_many":     "%d vấn đề",
	"tui.repos.local.problems_only":     "chỉ repo có vấn đề",
	"tui.repos.local.scanning":          "Đang quét %s · %s thư mục · %d repo",
	"tui.repos.local.scanning_wait":     "Đang quét…",
	"tui.repos.local.cancelled":         "Đã dừng quét. Bấm %[1]s để quét lại.",
	"tui.repos.local.no_company":        "Chưa có công ty nào. Thêm công ty ở màn hình Tài khoản trước.",
	"tui.repos.local.missing":           "%s chưa tồn tại. Clone một repo để tạo thư mục này.",
	"tui.repos.local.not_dir":           "%s không phải là thư mục.",
	"tui.repos.local.empty":             "Không có repo nào trong %s",
	"tui.repos.local.denied_one":        "1 thư mục không đọc được: %s",
	"tui.repos.local.denied_many":       "%d thư mục không đọc được: %s",
	"tui.repos.local.truncated":         "Đã dừng sau %s thư mục; đang hiện những gì tìm được.",
	"tui.repos.local.no_match":          "Không có gì khớp “%s”.",
	"tui.repos.local.no_problems":       "Không có vấn đề nào. Bấm %[1]s để hiện mọi repo.",
	"tui.repos.local.company_cancelled": "đã dừng quét",
	"tui.repos.local.filter_hint":       "để lọc theo đường dẫn, tài khoản, remote…",
	"tui.repos.local.clear_filter":      "(esc: bỏ lọc)",
	"tui.repos.local.none_selected":     "Chọn một repo để xem chi tiết.",
	// statuses, flags and kinds of a local repository
	"tui.repos.status.wrong_remote":    "sai username/alias",
	"tui.repos.status.unknown_account": "tài khoản lạ",
	"tui.repos.status.foreign_host":    "host chưa có tài khoản",
	"tui.repos.status.no_remote":       "không có remote",
	"tui.repos.status.unparsable":      "remote không đọc được",
	"tui.repos.status.ambiguous":       "chưa rõ tài khoản",
	"tui.repos.status.broken":          "lỗi git",
	"tui.repos.status.dubious":         "chủ sở hữu lạ",
	"tui.repos.flag.email_mismatch":    "email lệch",
	"tui.repos.flag.local_override":    "email riêng",
	"tui.repos.flag.wrong_folder":      "nên ở %s",
	"tui.repos.flag.duplicate":         "%d bản clone",
	"tui.repos.flag.via_symlink":       "symlink",
	"tui.repos.kind.worktree":          "worktree",
	"tui.repos.kind.submodule":         "submodule",
	"tui.repos.kind.bare":              "bare",

	// the hint line of the detail block
	"tui.repos.hint.wrong_remote":    "Remote chưa đúng dạng của tài khoản.",
	"tui.repos.hint.unknown_account": "Không có tài khoản nào dùng username này trên host này.",
	"tui.repos.hint.foreign_host":    "Chưa có tài khoản nào cho host này.",
	"tui.repos.hint.no_remote":       "Repo chưa có remote.",
	"tui.repos.hint.unparsable":      "Không đọc được URL của remote.",
	"tui.repos.hint.ambiguous":       "Có nhiều tài khoản khớp với remote này.",
	"tui.repos.hint.bare":            "Repo bare: không có working tree, không có gì để kiểm tra.",
	"tui.repos.hint.wrong_folder":    "Tài khoản của remote này thuộc công ty %s, không thuộc thư mục này.",
	"tui.repos.hint.duplicate":       "Cùng remote này còn được clone ở: %s",
	"tui.repos.hint.email_mismatch":  "Email đang dùng (%s) khác email của tài khoản (%s).",
	"tui.repos.hint.local_override":  "user.email được đặt trong .git/config của chính repo.",
	"tui.repos.hint.ok":              "Repo này đang dùng đúng tài khoản.",
	"tui.repos.copied":               "Đã sao chép: %s",
	"tui.repos.copy_failed":          "Không sao chép được. Đường dẫn là: %s",
	// scan (internal/app)
	"scan.detail.broken":  "git không đọc được repo này.",
	"scan.detail.dubious": "git từ chối repo này vì chủ sở hữu thư mục (safe.directory).",
}

func init() {
	for k, v := range viRepos {
		vi[k] = v
	}
}
