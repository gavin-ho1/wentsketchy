package icons

const (
	Apple           = ""
	Clock           = ""
	Chat            = "􀌤"
	Terminal        = ""

	// Filled in Icons
	Volume100       = "􀊩"
	Volume60        = "􀊧"
	Volume30        = "􀊥"
	VolumeMute      = "􀊣"

	// Not filled in Icons
	// Volume100       = "􀊨"
	// Volume60        = "􀊦"
	// Volume30        = "􀊤"
	// VolumeMute      = "􀊢"
	Code            = ""
	Chrome          = ""
	Finder          = ""
	Email           = ""
	Video           = "􀍉"
	Tools           = ""
	CPU             = "􀫥"
	ThermoMedium    = "􀇬"
	Documents       = "􀉁"
	Battery100      = "􀛨"
	Battery75       = "􀺸"
	Battery50       = "􀺶"
	Battery25       = "􀛩"
	Battery0        = "􀛪"
	BatteryCharging = "􀢋"
	LibreWolf       = "􀁟"
	Music           = ""
	Wifi            = ""
	WifiOff         = "􀙈"
	Bluetooth       = "󰂯"
	BluetoothOff    = "󰂲"
	Book            = "􀤞"
	Power           = "􀷄"
	None            = ""
	Unknown         = "󰀧"
	Tails           = ""
	Work            = ""
	Settings        = ""
	Restart         = "󰑓"

	// Media
	MediaPlay       = "􀊄"
	MediaPause      = "􀊆"
	MediaNext       = "􀊌"
	MediaPrevious   = "􀊊"
	MediaShuffle    = "􀊝"
	MediaRepeat     = "􀊞"
)

//nolint:gochecknoglobals // ok
var Workspace = map[string]string{
	"1": Work,
	"2": Code,
	"3": Chat,
	"4": Music,
	"5": Video,
	"6": Settings,
	// "7": Tools,
	// "8": Documents,
}

type IconInfo struct {
	Icon string
	Font string
}