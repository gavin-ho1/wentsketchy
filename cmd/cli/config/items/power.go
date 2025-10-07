package items

import (
	"context"
	"log/slog"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
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
	powerItemName = "power"
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
		ClickScript: `pmset displaysleepnow`,
	}

	itemArgs := powerItem.ToArgs()
	itemArgs = append(itemArgs,
		"padding_left=-10",
		"padding_right=-10",
		"icon.font.size=24.0",
		"background.drawing=off",
		"border.drawing=off",
	)

	batches = batch(batches, s("--add", "item", powerItemName, position))
	batches = batch(batches, m(s("--set", powerItemName), itemArgs))

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