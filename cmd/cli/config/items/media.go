package items

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/colors"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/command"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
)

type MediaItem struct {
	logger  *slog.Logger
	command *command.Command
}

func NewMediaItem(
	logger *slog.Logger,
	command *command.Command,
) MediaItem {
	return MediaItem{logger, command}
}

const (
	mediaItemName        = "media"
	mediaEvent           = "media_change"
	mediaCheckerItemName = "media.checker"

	mediaPrevItemName      = "media.prev"
	mediaPlayPauseItemName = "media.playpause"
	mediaNextItemName      = "media.next"
	mediaInfoItemName      = "media.info"
	mediaBracketItemName   = "media.bracket"
)

func (i MediaItem) Init(
	_ context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.Error("media: recovered from panic in Init", slog.Any("panic", r))
		}
	}()
	updateEvent, err := args.BuildEvent()
	if err != nil {
		i.logger.Error("media: could not generate update event", slog.Any("error", err))
		return batches, nil
	}

	// This item is just a checker to trigger updates. It's not visible.
	checkerItem := sketchybar.ItemOptions{
		Updates:    "on",
		Script:     updateEvent,
		UpdateFreq: pointer(120),
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}
	batches = batch(batches, s("--add", "item", mediaCheckerItemName, position))
	batches = batch(batches, m(s("--set", mediaCheckerItemName), checkerItem.ToArgs()))
	batches = batch(batches, s("--subscribe", mediaCheckerItemName, events.SystemWoke, mediaEvent, "routine", "forced"))

	nextItem := sketchybar.ItemOptions{
		Display: "active",
		Icon: sketchybar.ItemIconOptions{
			Value: icons.MediaNext,
			Font: sketchybar.FontOptions{
				Font: settings.FontIcon,
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: settings.Sketchybar.IconPadding,
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Drawing: "off",
		},
		ClickScript: `osascript -e 'tell application "Spotify" to next track' && sketchybar --trigger media_change`,
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}
	batches = batch(batches, s("--add", "item", mediaNextItemName, position))
	batches = batch(batches, m(s("--set", mediaNextItemName), nextItem.ToArgs()))

	playPauseItem := sketchybar.ItemOptions{
		Display: "active",
		Icon: sketchybar.ItemIconOptions{
			Value: icons.MediaPlay,
			Font: sketchybar.FontOptions{
				Font: settings.FontIcon,
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: settings.Sketchybar.IconPadding,
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Drawing: "off",
		},
		ClickScript: `
			PLAYER_STATE=$(osascript -e 'tell application "Spotify" to player state as string')
			if [ "$PLAYER_STATE" = "playing" ]; then
				osascript -e 'tell application "Spotify" to pause'
				sketchybar --set media.info label=""
			else
				osascript -e 'tell application "Spotify" to play'
				TRACK=$(osascript -e 'tell application "Spotify" to name of current track')
				ARTIST=$(osascript -e 'tell application "Spotify" to artist of current track')
				LABEL="$TRACK • $ARTIST"
				LENGTH=${#LABEL}
				if [ $LENGTH -gt 40 ]; then
					LABEL=${LABEL:0:37}"…"
				fi
				sketchybar --set media.info label="$LABEL"
			fi
			sketchybar --trigger media_change
		`,
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}
	batches = batch(batches, s("--add", "item", mediaPlayPauseItemName, position))
	batches = batch(batches, m(s("--set", mediaPlayPauseItemName), playPauseItem.ToArgs()))

	prevItem := sketchybar.ItemOptions{
		Display: "active",
		Icon: sketchybar.ItemIconOptions{
			Value: icons.MediaPrevious,
			Font: sketchybar.FontOptions{
				Font: settings.FontIcon,
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: settings.Sketchybar.IconPadding,
			},
		},
		Label: sketchybar.ItemLabelOptions{
			Drawing: "off",
		},
		ClickScript: `osascript -e 'tell application "Spotify" to previous track' && sketchybar --trigger media_change`,
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}
	batches = batch(batches, s("--add", "item", mediaPrevItemName, position))
	batches = batch(batches, m(s("--set", mediaPrevItemName), prevItem.ToArgs()))

	infoItem := sketchybar.ItemOptions{
		Display: "active",
		Label: sketchybar.ItemLabelOptions{
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.IconPadding,
				Right: settings.Sketchybar.IconPadding,
			},
		},
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}
	batches = batch(batches, s("--add", "item", mediaInfoItemName, position))
	batches = batch(batches, m(s("--set", mediaInfoItemName), infoItem.ToArgs()))

	bracketItem := sketchybar.BracketOptions{
		Background: sketchybar.BackgroundOptions{
			Drawing: "on",
			Color: sketchybar.ColorOptions{
				Color: colors.Transparent,
			},
		},
	}
	batches = batch(batches, s(
		"--add",
		"bracket",
		mediaBracketItemName,
		mediaNextItemName,
		mediaPlayPauseItemName,
		mediaPrevItemName,
		mediaInfoItemName,
	))
	batches = batch(batches, m(s("--set", mediaBracketItemName), bracketItem.ToArgs()))

	return batches, nil
}

func (i MediaItem) Update(
	ctx context.Context,
	batches Batches,
	_ sketchybar.Position,
	args *args.In,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.ErrorContext(ctx, "media: recovered from panic in Update", slog.Any("panic", r))
		}
	}()
	if args.Name != mediaCheckerItemName {
		return batches, nil
	}

	itemsToManage := []string{
		mediaPrevItemName,
		mediaPlayPauseItemName,
		mediaNextItemName,
		mediaInfoItemName,
		mediaBracketItemName,
	}

	playerState, err := i.command.Run(ctx, "osascript", "-e", `tell application "Spotify" to player state as string`)
	if err != nil {
		i.logger.DebugContext(ctx, "media: could not get player state", slog.Any("error", err))
		for _, item := range itemsToManage {
			batches = batch(batches, s("--set", item, "drawing=off"))
		}
		return batches, nil
	}

	for _, item := range itemsToManage {
		batches = batch(batches, s("--set", item, "drawing=on"))
	}

	track, _ := i.command.Run(ctx, "osascript", "-e", `tell application "Spotify" to name of current track`)
	artist, _ := i.command.Run(ctx, "osascript", "-e", `tell application "Spotify" to artist of current track`)

	cleanTrack := strings.ReplaceAll(strings.TrimSpace(track), "\"", "")
	cleanArtist := strings.ReplaceAll(strings.TrimSpace(artist), "\"", "")
	label := fmt.Sprintf("%s • %s", cleanTrack, cleanArtist)
	if len(label) > 40 {
		label = label[:37] + "..."
	}

	trimmedState := strings.TrimSpace(playerState)
	if trimmedState == "playing" {
		playPauseItem := sketchybar.ItemOptions{
			Icon: sketchybar.ItemIconOptions{Value: icons.MediaPause},
		}
		batches = batch(batches, m(s("--set", mediaPlayPauseItemName), playPauseItem.ToArgs()))

		infoItem := sketchybar.ItemOptions{
			Label: sketchybar.ItemLabelOptions{Value: label},
		}
		batches = batch(batches, m(s("--set", mediaInfoItemName), infoItem.ToArgs()))

	} else if trimmedState == "paused" {
		playPauseItem := sketchybar.ItemOptions{
			Icon: sketchybar.ItemIconOptions{Value: icons.MediaPlay},
		}
		batches = batch(batches, m(s("--set", mediaPlayPauseItemName), playPauseItem.ToArgs()))

		infoItem := sketchybar.ItemOptions{
			Label: sketchybar.ItemLabelOptions{Value: label},
		}
		batches = batch(batches, m(s("--set", mediaInfoItemName), infoItem.ToArgs()))
	} else {
		for _, item := range itemsToManage {
			batches = batch(batches, s("--set", item, "drawing=off"))
		}
	}

	return batches, nil
}

var _ WentsketchyItem = (*MediaItem)(nil)
