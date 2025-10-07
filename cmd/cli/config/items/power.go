package items

import (
	"context"
	"log/slog"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/colors"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/command"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
)

type PowerItem struct {
	logger  *slog.Logger
	command *command.Command
}

func NewPowerItem(logger *slog.Logger, command *command.Command) PowerItem {
	return PowerItem{logger, command}
}

const (
	powerItemName         = "power"
	powerSleepItemName    = "power.sleep"
	powerShutdownItemName = "power.shutdown"
	powerRestartItemName  = "power.restart"
)

func (i PowerItem) Init(
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.Error("power: recovered from panic in Init", slog.Any("panic", r))
		}
	}()

	powerItem := sketchybar.ItemOptions{
		Display: "active",
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Power,
			Font: sketchybar.FontOptions{
				Font: settings.Sketchybar.IconFont,
				Kind: settings.Sketchybar.IconFontKind,
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: settings.Sketchybar.IconPadding,
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Drawing: "off",
		},
		ClickScript: `sketchybar --set power popup.drawing=toggle`,
	}

	popupAlign := "center"
	if position == sketchybar.PositionLeft || position == sketchybar.PositionLeftNotch {
		popupAlign = "left"
	} else if position == sketchybar.PositionRight || position == sketchybar.PositionRightNotch {
		popupAlign = "right"
	}

	itemArgs := powerItem.ToArgs()
	itemArgs = append(itemArgs,
		"padding_left=-10",
		"padding_right=-10",
		"icon.font.size=24.0",
		"background.drawing=off",
		"border.drawing=off",
		"popup.align="+popupAlign,
		"popup.sticky=off",
		"popup.background.drawing=on",
		"popup.background.corner_radius=8",
		"popup.background.border_width=3",
		"popup.background.border_color="+colors.White,
		"popup.background.color="+colors.Black1,
		"popup.background.padding_left=20",
		"popup.background.padding_right=20",
	)

	batches = batch(batches, s("--add", "item", powerItemName, position))
	batches = batch(batches, m(s("--set", powerItemName), itemArgs))
	batches = batch(batches, s("--add", "item", powerSleepItemName, "popup."+powerItemName))
	batches = batch(batches, s("--add", "item", powerShutdownItemName, "popup."+powerItemName))
	batches = batch(batches, s("--add", "item", powerRestartItemName, "popup."+powerItemName))

	popupItemOptions := []string{"background.drawing=off"}

	sleepItem := sketchybar.ItemOptions{
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Clock,
		},
		Label: sketchybar.ItemLabelOptions{
			Value: "Sleep",
		},
		ClickScript: `pmset displaysleepnow && sketchybar --set power popup.drawing=off`,
	}
	sleepArgs := append(sleepItem.ToArgs(), popupItemOptions...)
	sleepArgs = append(sleepArgs, "icon.padding_left=10", "label.padding_left=10", "label.padding_right=10")
	batches = batch(batches, m(s("--set", powerSleepItemName), sleepArgs))

	shutdownItem := sketchybar.ItemOptions{
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Power,
		},
		Label: sketchybar.ItemLabelOptions{
			Value: "Shutdown",
		},
		ClickScript: `sudo shutdown -h now && sketchybar --set power popup.drawing=off`,
	}
	shutdownArgs := append(shutdownItem.ToArgs(), popupItemOptions...)
	shutdownArgs = append(shutdownArgs, "icon.padding_left=9", "label.padding_left=10", "label.padding_right=10")
	batches = batch(batches, m(s("--set", powerShutdownItemName), shutdownArgs))

	restartItem := sketchybar.ItemOptions{
		Icon: sketchybar.ItemIconOptions{
			Value: icons.Restart,
		},
		Label: sketchybar.ItemLabelOptions{
			Value: "Restart",
		},
		ClickScript: `sudo shutdown -r now && sketchybar --set power popup.drawing=off`,
	}
	restartArgs := append(restartItem.ToArgs(), popupItemOptions...)
	restartArgs = append(restartArgs, "icon.padding_left=10", "label.padding_left=10", "label.padding_right=10")
	batches = batch(batches, m(s("--set", powerRestartItemName), restartArgs))

	return batches, nil
}

func (i PowerItem) Update(
	ctx context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	// No-op
	return batches, nil
}

var _ WentsketchyItem = (*PowerItem)(nil)