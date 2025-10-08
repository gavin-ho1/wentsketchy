package items

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/colors"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/command"
	"github.com/lucax88x/wentsketchy/internal/encoding"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
)

type MediaItem struct {
	logger         *slog.Logger
	command        *command.Command
	mu             sync.Mutex
	isPlayerActive bool
	currentWidth   int
	currentLabel   string
}

func NewMediaItem(
	logger *slog.Logger,
	command *command.Command,
) *MediaItem {
	return &MediaItem{
		logger:  logger,
		command: command,
	}
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

	avgCharWidth = 7
)

func (i *MediaItem) Init(
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

	checkerItem := sketchybar.ItemOptions{
		Updates:    "on",
		Script:     updateEvent,
		UpdateFreq: pointer(120),
		Background: sketchybar.BackgroundOptions{Drawing: "off"},
	}
	batches = batch(batches, s("--add", "item", mediaCheckerItemName, position))
	batches = batch(batches, m(s("--set", mediaCheckerItemName), checkerItem.ToArgs()))
	batches = batch(batches, s("--subscribe", mediaCheckerItemName, events.SystemWoke, mediaEvent, "routine", "forced"))

	nextItem := sketchybar.ItemOptions{
		Display:     "active",
		Icon:        sketchybar.ItemIconOptions{Value: icons.MediaNext, Font: sketchybar.FontOptions{Font: settings.FontIcon}, Padding: sketchybar.PaddingOptions{Left: pointer(0), Right: settings.Sketchybar.IconPadding}},
		Label:       sketchybar.ItemLabelOptions{Drawing: "off"},
		ClickScript: `osascript -e 'tell application "Spotify" to next track' && sketchybar --trigger media_change`,
		Background:  sketchybar.BackgroundOptions{Drawing: "off"},
	}
	batches = batch(batches, s("--add", "item", mediaNextItemName, position))
	batches = batch(batches, m(s("--set", mediaNextItemName), nextItem.ToArgs()))

	playPauseItem := sketchybar.ItemOptions{
		Display:     "active",
		Icon:        sketchybar.ItemIconOptions{Value: icons.MediaPlay, Font: sketchybar.FontOptions{Font: settings.FontIcon}, Padding: sketchybar.PaddingOptions{Left: settings.Sketchybar.IconPadding, Right: settings.Sketchybar.IconPadding}},
		Label:       sketchybar.ItemLabelOptions{Drawing: "off"},
		ClickScript: `osascript -e 'tell application "Spotify" to playpause' && sketchybar --trigger media_change`,
		Background:  sketchybar.BackgroundOptions{Drawing: "off"},
	}
	batches = batch(batches, s("--add", "item", mediaPlayPauseItemName, position))
	batches = batch(batches, m(s("--set", mediaPlayPauseItemName), playPauseItem.ToArgs()))

	prevItem := sketchybar.ItemOptions{
		Display:     "active",
		Icon:        sketchybar.ItemIconOptions{Value: icons.MediaPrevious, Font: sketchybar.FontOptions{Font: settings.FontIcon}, Padding: sketchybar.PaddingOptions{Left: settings.Sketchybar.IconPadding, Right: pointer(0)}},
		Label:       sketchybar.ItemLabelOptions{Drawing: "off"},
		ClickScript: `osascript -e 'tell application "Spotify" to previous track' && sketchybar --trigger media_change`,
		Background:  sketchybar.BackgroundOptions{Drawing: "off"},
	}
	batches = batch(batches, s("--add", "item", mediaPrevItemName, position))
	batches = batch(batches, m(s("--set", mediaPrevItemName), prevItem.ToArgs()))

	infoItem := sketchybar.ItemOptions{
		Display:     "active",
		Width:       pointer(0),
		ScrollTexts: "off",
		Label: sketchybar.ItemLabelOptions{
			Drawing: "off",
			Padding: sketchybar.PaddingOptions{Left: settings.Sketchybar.IconPadding, Right: pointer(1)},
		},
		Background: sketchybar.BackgroundOptions{Drawing: "off"},
	}
	batches = batch(batches, s("--add", "item", mediaInfoItemName, position))
	batches = batch(batches, m(s("--set", mediaInfoItemName), infoItem.ToArgs()))

	bracketItem := sketchybar.BracketOptions{
		Background: sketchybar.BackgroundOptions{
			Drawing: "on",
			Color:   sketchybar.ColorOptions{Color: colors.Transparent},
			Border:  sketchybar.BorderOptions{Color: colors.WhiteA05},
		},
	}
	batches = batch(batches, s(
		"--add", "bracket", mediaBracketItemName,
		mediaPrevItemName, mediaPlayPauseItemName, mediaNextItemName, mediaInfoItemName,
	))
	batches = batch(batches, m(s("--set", mediaBracketItemName), bracketItem.ToArgs()))

	return batches, nil
}

func (i *MediaItem) Update(
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

	i.mu.Lock()
	defer i.mu.Unlock()

	itemsToManage := []string{
		mediaPrevItemName, mediaPlayPauseItemName, mediaNextItemName,
		mediaInfoItemName, mediaBracketItemName,
	}

	playerState, err := i.command.Run(ctx, "osascript", "-e", `tell application "Spotify" to player state as string`)
	trimmedState := strings.TrimSpace(playerState)

	if err != nil || (trimmedState != "playing" && trimmedState != "paused") {
		if i.isPlayerActive {
			for _, item := range itemsToManage {
				batches = batch(batches, s("--set", item, "drawing=off"))
			}
			i.isPlayerActive = false
			i.currentWidth = 0
			i.currentLabel = ""
		}
		return batches, nil
	}

	if !i.isPlayerActive {
		for _, item := range itemsToManage {
			batches = batch(batches, s("--set", item, "drawing=on"))
		}
		i.isPlayerActive = true
	}

	var targetWidth int
	var newLabel string
	var isPlaying bool

	if trimmedState == "playing" {
		trackBuff, _ := i.command.RunBufferized(ctx, "osascript", "-e", `tell application "Spotify" to name of current track`)
		artistBuff, _ := i.command.RunBufferized(ctx, "osascript", "-e", `tell application "Spotify" to artist of current track`)
		track, _ := encoding.DecodeAppleScriptOutput(trackBuff.Bytes())
		artist, _ := encoding.DecodeAppleScriptOutput(artistBuff.Bytes())

		cleanLabel := fmt.Sprintf("%s • %s", strings.TrimSpace(track), strings.TrimSpace(artist))
		cleanLabel = strings.ReplaceAll(cleanLabel, "\"", "")
		cleanLabel = strings.ReplaceAll(cleanLabel, "'", "")

		if len([]rune(cleanLabel)) > 40 {
			newLabel = string([]rune(cleanLabel)[:37]) + "..."
		} else {
			newLabel = cleanLabel
		}
		labelRunes := []rune(newLabel)
		targetWidth = len(labelRunes)*avgCharWidth + *settings.Sketchybar.IconPadding + 1
		isPlaying = true
	} else {
		newLabel = ""
		targetWidth = 0
		isPlaying = false
	}

	if targetWidth != i.currentWidth || newLabel != i.currentLabel {
		var animationArgs []string
		if targetWidth > i.currentWidth {
			animationArgs = []string{
				"label.align=right",
				fmt.Sprintf("label=\"%s\"", newLabel),
				"label.drawing=on",
				"label.max_chars=" + strconv.Itoa(len([]rune(newLabel))),
				"width=" + strconv.Itoa(targetWidth),
			}
		} else {
			animationArgs = []string{
				"label.align=left",
				fmt.Sprintf("label=\"%s\"", newLabel),
				"label.max_chars=" + strconv.Itoa(len([]rune(newLabel))),
				"width=" + strconv.Itoa(targetWidth),
			}
			if targetWidth == 0 {
				animationArgs = append(animationArgs, "label.drawing=off")
			}
		}
		batches = batch(batches, m(s("--animate", sketchybar.AnimationTanh, "15", "--set", mediaInfoItemName), animationArgs))
		i.currentWidth = targetWidth
		i.currentLabel = newLabel
	}

	if isPlaying {
		playPauseItem := sketchybar.ItemOptions{Icon: sketchybar.ItemIconOptions{Value: icons.MediaPause}}
		batches = batch(batches, m(s("--set", mediaPlayPauseItemName), playPauseItem.ToArgs()))
	} else {
		playPauseItem := sketchybar.ItemOptions{Icon: sketchybar.ItemIconOptions{Value: icons.MediaPlay}}
		batches = batch(batches, m(s("--set", mediaPlayPauseItemName), playPauseItem.ToArgs()))
	}

	return batches, nil
}

var _ WentsketchyItem = (*MediaItem)(nil)