package i18n

// viHome holds the Vietnamese strings of the start menu (internal/tui/home.go).
var viHome = map[string]string{
	"tui.home.title":   "Trang chủ",
	"tui.home.tagline": "Đúng tài khoản Git cho từng repo.",

	"tui.home.item.accounts":            "Tài khoản",
	"tui.home.item.clone":               "Clone repo",
	"tui.home.item.check":               "Kiểm tra repo",
	"tui.home.item.repos":               "Duyệt repo",
	"tui.home.item.apply":               "Áp dụng",
	"tui.home.item.doctor":              "Chẩn đoán",
	"tui.home.item.import":              "Nhập từ git",
	"tui.home.item.accounts.desc_empty": "Bắt đầu ở đây: thêm công ty đầu tiên",
	"tui.home.item.accounts.desc":       "Công ty, tài khoản và SSH key",
	"tui.home.item.clone.desc":          "Vào đúng thư mục, với đúng tài khoản",
	"tui.home.item.check.desc":          "Repo ở thư mục này dùng tài khoản nào",
	"tui.home.item.repos.desc":          "Repo trên máy và trên các nền tảng",
	"tui.home.item.apply.desc":          "Xem trước rồi ghi vào ~/.gitconfig và ~/.ssh",
	"tui.home.item.doctor.desc":         "Kiểm tra git, SSH và cấu hình",
	"tui.home.item.import.desc":         "Đọc includeIf có sẵn trong ~/.gitconfig",

	// the update line; the first argument is a version (digits and dots)
	"tui.home.update.brew":    "Có bản mới %s · chạy brew upgrade gitid",
	"tui.home.update.script":  "Có bản mới %s · cập nhật bằng install.sh như lúc cài",
	"tui.home.update.unknown": "Có bản mới %s · xem github.com/tinhnd1902/gitid/releases",

	// the command inside the line above (shown in bold); not translated
	"tui.home.update.brew_cmd":   "brew upgrade gitid",
	"tui.home.update.script_cmd": "install.sh",

	"tui.key.home_open": "mở",
	"tui.key.jump":      "chọn nhanh",
}

func init() {
	for k, v := range viHome {
		vi[k] = v
	}
}
