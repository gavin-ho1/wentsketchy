package settings

import "github.com/lucax88x/wentsketchy/cmd/cli/config/settings/colors"

type AerospaceSettings struct {
	Padding *int

	WorkspaceBackgroundColor        string
	WorkspaceColor                  string
	WorkspaceFocusedBackgroundColor string
	WorkspaceFocusedColor           string
	WindowColor                     string
	WindowFocusedColor              string
	TransitionTime                  string
}

type Settings struct {
	BarBackgroundColor  string
	BarHeight           *int
	BarMargin           *int
	BarTransitionTime   string
	ItemHeight          *int
	ItemSpacing         *int
	ItemRadius          *int
	ItemBackgroundColor string
	ItemBorderColor     string
	IconPadding         *int
	LabelColor          string
	LabelFont           string
	LabelFontKind       string
	LabelFontSize       string
	IconColor           string
	IconFont            string
	IconFontKind        string
	IconFontSize        string
	IconStripFont       string
	Aerospace           AerospaceSettings
}

//nolint:gochecknoglobals // ok
var Sketchybar = Settings{
	BarBackgroundColor:  colors.Transparent,
	BarHeight:           pointer(35),
	BarMargin:           pointer(0),
	BarTransitionTime:   "60",
	ItemHeight:          pointer(25),
	ItemSpacing:         pointer(2),
	ItemRadius:          pointer(45),
	IconPadding:         pointer(12),
	ItemBackgroundColor: colors.Transparent,
	ItemBorderColor:     colors.WhiteA40,
	LabelColor:          colors.White,
	LabelFont:           FontLabel,
	LabelFontKind:       "Medium",
	LabelFontSize:       "14.0",
	IconColor:           colors.White,
	IconFont:            FontIcon,
	IconFontKind:        "Bold",
	IconFontSize:        "16.0",
	IconStripFont:       FontAppIcon,
	Aerospace: AerospaceSettings{
		Padding:                         pointer(8),
		WorkspaceBackgroundColor:        colors.Transparent,
		WorkspaceColor:                  colors.WhiteA40,
		WorkspaceFocusedBackgroundColor: colors.White,
		WorkspaceFocusedColor:           colors.Black1,
		WindowColor:                     colors.WhiteA40,
		WindowFocusedColor:              colors.White,
		TransitionTime:                  "1",
	},
}

func pointer(i int) *int {
	return &i
}
