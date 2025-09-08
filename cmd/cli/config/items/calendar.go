package items

import (
	"context"
	"log/slog"
	"time"
	"fmt"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	// "github.com/lucax88x/wentsketchy/internal/formatter"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
)
  
type CalendarItem struct {
	logger *slog.Logger
}

func NewCalendarItem(logger *slog.Logger) CalendarItem {
	return CalendarItem{logger}
}

const calendarItemName = "calendar"

func (i CalendarItem) Init(
	ctx context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.Error("calendar: recovered from panic in Init", slog.Any("panic", r))
		}
	}()
	
	// Use a simple shell script that updates the time directly
	updateScript := `#!/bin/bash
TIME=$(date "+%b %e %l:%M %p" | sed -e 's/  / /g')
sketchybar --set "$NAME" label="$TIME"`

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
			Value: "Loading...",
			Padding: sketchybar.PaddingOptions{
				Left:  pointer(0),
				Right: settings.Sketchybar.IconPadding,
			},
		},
		UpdateFreq: pointer(1), // Update every minute
		Updates:    "on",
		Script:     updateScript, // Use inline script for time updates
	}

	batches = batch(batches, s("--add", "item", calendarItemName, position))
	batches = batch(batches, m(s("--set", calendarItemName), calendarItem.ToArgs()))
	batches = batch(batches, s("--subscribe", calendarItemName, events.SystemWoke))

	return batches, nil
}

func (i CalendarItem) Update(
	ctx context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	// Handle system wake events since routine updates are handled by inline script
	defer func() {
		if r := recover(); r != nil {
			i.logger.ErrorContext(ctx, "calendar: recovered from panic in Update", slog.Any("panic", r))
		}
	}()
	
	if !isCalendar(args.Name) {
		return batches, nil
	}

	if args.Event == events.SystemWoke {
		now := time.Now()
		hour := now.Hour() % 12
		if hour == 0 {
			hour = 12
		}
		formattedTime := fmt.Sprintf("%s %d:%02d %s", now.Format("Jan 2"), hour, now.Minute(), now.Format("PM"))
	
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