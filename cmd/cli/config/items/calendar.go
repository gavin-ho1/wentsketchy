package items

import (
	"context"
	"errors"
	"time"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/formatter"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
)

type CalendarItem struct {
}

func NewCalendarItem() CalendarItem {
	return CalendarItem{}
}

const calendarItemName = "calendar"

func (i CalendarItem) Init(
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	updateEvent, err := args.BuildEvent()

	if err != nil {
		return batches, errors.New("calendar: could not generate update event")
	}

	calendarItem := sketchybar.ItemOptions{
		Display: "active",
		Padding: sketchybar.PaddingOptions{
			Left:  settings.Sketchybar.ItemSpacing,
			Right: settings.Sketchybar.ItemSpacing,
		},
		Icon: sketchybar.ItemIconOptions{
			Value: icons.None,
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(*settings.Sketchybar.IconPadding / 2),
				Right: pointer(*settings.Sketchybar.IconPadding / 2),
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(0),
				Right: settings.Sketchybar.IconPadding,
			},
		},
		UpdateFreq: pointer(30),
		Updates:    "on",
		Script:     updateEvent,
		// Click_script:            "$PLUGIN_DIR/zen.sh",
	}

	batches = batch(batches, s("--add", "item", calendarItemName, position))
	batches = batch(batches, m(s("--set", calendarItemName), calendarItem.ToArgs()))
	batches = batch(batches, s("--subscribe", calendarItemName, events.SystemWoke))

	return batches, nil
}

func (i CalendarItem) Update(
	_ context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	if !isCalendar(args.Name) {
		return batches, nil
	}

	if args.Event == events.Routine || args.Event == events.Forced {
		now := time.Now()
		day := now.Day()
		suffix := "th"
		switch day {
		case 1, 21, 31:
			suffix = "st"
		case 2, 22:
			suffix = "nd"
		case 3, 23:
			suffix = "rd"
		}

		formattedTime := now.Format("Mon Jan ") + formatter.Int(day) + suffix + now.Format(" | 3:04 PM")

		calendarItem := sketchybar.ItemOptions{
			Label: sketchybar.ItemLabelOptions{
				Value: formattedTime,
			},
		}

		batches = batch(batches, m(s("--set", calendarItemName), calendarItem.ToArgs()))
	}

	return batches, nil
}

func isCalendar(name string) bool {
	return name == calendarItemName
}

var _ WentsketchyItem = (*CalendarItem)(nil)
