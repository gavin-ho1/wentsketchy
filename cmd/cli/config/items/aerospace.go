package items

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings"
	colorsPkg "github.com/lucax88x/wentsketchy/cmd/cli/config/settings/colors"
	"github.com/lucax88x/wentsketchy/cmd/cli/config/settings/icons"
	"github.com/lucax88x/wentsketchy/internal/aerospace"
	aerospace_events "github.com/lucax88x/wentsketchy/internal/aerospace/events"
	"github.com/lucax88x/wentsketchy/internal/sketchybar"
	"github.com/lucax88x/wentsketchy/internal/sketchybar/events"
	"github.com/lucax88x/wentsketchy/internal/utils"
)

type AerospaceItem struct {
	logger       *slog.Logger
	aerospace    aerospace.Aerospace
	sketchybar   sketchybar.API
	position     sketchybar.Position
	windowIDs    map[aerospace.WindowID]aerospace.WorkspaceID
	syncCallback func(context.Context, *args.In)
	pollTimer    *time.Timer
	pollMutex    sync.Mutex
	lastPollTime time.Time
	isPolling    bool
}

const (
	pollDelay    = 100 * time.Millisecond
	pollDebounce = 10 * time.Millisecond
)

func (item *AerospaceItem) startDelayedPoll(ctx context.Context) {
	item.pollMutex.Lock()
	defer item.pollMutex.Unlock()

	if item.pollTimer != nil {
		item.pollTimer.Stop()
	}

	if time.Since(item.lastPollTime) < pollDebounce {
		return
	}

	item.pollTimer = time.AfterFunc(pollDelay, func() {
		item.pollMutex.Lock()
		defer item.pollMutex.Unlock()

		if item.isPolling {
			return
		}

		item.isPolling = true
		item.lastPollTime = time.Now()

		go func() {
			defer func() {
				item.pollMutex.Lock()
				item.isPolling = false
				item.pollMutex.Unlock()
			}()

			item.logger.Debug("Starting delayed poll for aerospace sync")

			syntheticArgs := &args.In{
				Name:  AerospaceName,
				Event: events.SpaceWindowsChange,
				Info:  "",
			}

			if callback := item.syncCallback; callback != nil {
				callback(ctx, syntheticArgs)
			}
		}()
	})
}

func (item *AerospaceItem) handleSyncCallback(ctx context.Context, args *args.In) {
	batches := make(Batches, 0)
	batches, err := item.Update(ctx, batches, item.position, args)
	if err != nil {
		item.logger.ErrorContext(ctx, "syncCallback: error updating item", slog.Any("err", err))
		return
	}

	err = item.sketchybar.Run(ctx, Flatten(batches...))
	if err != nil {
		item.logger.ErrorContext(ctx, "syncCallback: error running sketchybar", slog.Any("err", err))
	}
}

func (item *AerospaceItem) performSyncCheck(ctx context.Context, batches Batches) (Batches, error) {
	item.logger.Debug("Performing sync check")
	item.aerospace.SingleFlightRefreshTree()
	return item.CheckTree(ctx, batches)
}

func NewAerospaceItem(
	logger *slog.Logger,
	aerospace aerospace.Aerospace,
	sketchybarAPI sketchybar.API,
) *AerospaceItem {
	item := &AerospaceItem{
		logger:       logger,
		aerospace:    aerospace,
		sketchybar:   sketchybarAPI,
		position:     sketchybar.PositionLeft,
		windowIDs:    make(map[int]string, 0),
		pollMutex:    sync.Mutex{},
		lastPollTime: time.Time{},
		isPolling:    false,
	}
	item.syncCallback = item.handleSyncCallback
	return item
}

const aerospaceCheckerItemName = "aerospace.checker"
const workspaceItemPrefix = "aerospace.workspace"
const windowItemPrefix = "aerospace.window"
const bracketItemPrefix = "aerospace.bracket"
const spacerItemPrefix = "aerospace.spacer"

const AerospaceName = aerospaceCheckerItemName

func (item *AerospaceItem) Init(
	ctx context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	item.position = position

	batches, err := item.applyTree(ctx, batches, position)
	if err != nil {
		return batches, err
	}

	batches, err = checker(batches, position)
	if err != nil {
		return batches, err
	}

	return batches, nil
}

func (item *AerospaceItem) Update(
	ctx context.Context,
	batches Batches,
	position sketchybar.Position,
	args *args.In,
) (Batches, error) {
	item.position = position

	if !isAerospace(args.Name) {
		return batches, nil
	}

	var err error
	switch args.Event {
	case aerospace_events.WorkspaceChange:
		var data aerospace_events.WorkspaceChangeEventInfo
		err = json.Unmarshal([]byte(args.Info), &data)
		if err != nil {
			return batches, fmt.Errorf("aerospace: could not deserialize json for workspace-change. %v", args.Info)
		}
		item.aerospace.SetFocusedWorkspaceID(data.Focused)
		batches = item.handleWorkspaceChange(ctx, batches, data.Prev, data.Focused)
	case events.SpaceWindowsChange:
		batches, err = item.CheckTree(ctx, batches)
		if err == nil {
			item.startDelayedPoll(ctx)
		}
	case events.DisplayChange:
		batches = item.handleDisplayChange(batches)
		item.startDelayedPoll(ctx)
	case events.FrontAppSwitched:
		batches = item.handleFrontAppSwitched(ctx, batches, args.Info)
		item.startDelayedPoll(ctx)
	}

	return batches, err
}

func (item *AerospaceItem) updateWorkspaceBracket(
	batches Batches,
	workspaceID aerospace.WorkspaceID,
	isFocused bool,
) Batches {
	tree := item.aerospace.GetTree()
	if tree == nil {
		return batches
	}

	workspace, found := tree.IndexedWorkspaces[workspaceID]
	sketchybarBracketID := getSketchybarBracketID(workspaceID)

	if !found || len(workspace.Windows) == 0 {
		batches = batch(batches, s("--remove", sketchybarBracketID))
		return batches
	}

	batches = batch(batches, s("--remove", sketchybarBracketID))

	members := []string{getSketchybarWorkspaceID(workspaceID)}
	for _, windowID := range workspace.Windows {
		members = append(members, getSketchybarWindowID(windowID))
	}

	batches = batch(batches, m(s(
		"--add", "bracket", sketchybarBracketID, getSketchybarWorkspaceID(workspaceID)),
		members[1:],
	))

	colors := item.getWorkspaceColors(isFocused)
	workspaceBracketItem := sketchybar.BracketOptions{
		Background: sketchybar.BackgroundOptions{
			Drawing: "on",
			Border: sketchybar.BorderOptions{
				Color: colors.backgroundColor,
			},
			Color: sketchybar.ColorOptions{
				Color: colorsPkg.Transparent,
			},
		},
	}
	batches = batch(batches, m(s("--set", sketchybarBracketID), workspaceBracketItem.ToArgs()))

	return batches
}

func (item *AerospaceItem) CheckTree(
	ctx context.Context,
	batches Batches,
) (Batches, error) {
	item.logger.Info("Checking aerospace tree for updates")
	item.aerospace.SingleFlightRefreshTree()
	tree := item.aerospace.GetTree()

	if tree == nil {
		item.logger.Error("Tree is nil after refresh")
		return batches, errors.New("aerospace tree is nil")
	}

	focusedWorkspaceID := item.aerospace.GetFocusedWorkspaceID(ctx)
	item.logger.Debug("Focused workspace", slog.String("workspace", focusedWorkspaceID))

	var aggregatedErr error
	workspacesNeedingUpdate := make(map[aerospace.WorkspaceID]bool)

	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue
			}

			isFocusedWorkspace := workspace.Workspace == focusedWorkspaceID

			for _, windowID := range workspace.Windows {
				window := tree.IndexedWindows[windowID]
				if window == nil {
					item.logger.Warn("Window not found in tree", slog.Int("windowID", windowID))
					continue
				}

				currentWorkspaceID, found := item.windowIDs[windowID]

				if found {
					if currentWorkspaceID != workspace.Workspace {
						item.logger.Info("Moving window to new workspace",
							slog.Int("window.id", window.ID),
							slog.String("window.app", window.App),
							slog.String("old.workspace", currentWorkspaceID),
							slog.String("new.workspace", workspace.Workspace),
						)

						workspacesNeedingUpdate[currentWorkspaceID] = true
						workspacesNeedingUpdate[workspace.Workspace] = true

						windowItem := item.windowToSketchybar(
							isFocusedWorkspace,
							monitor.Monitor,
							workspace.Workspace,
							window.App,
						)

						batches = batch(batches, m(
							s("--set", getSketchybarWindowID(windowID)),
							windowItem.ToArgs(),
						))

						batches = batch(batches, s(
							"--move",
							getSketchybarWindowID(windowID),
							"after",
							getSketchybarWorkspaceID(workspace.Workspace),
						))

						batches = batch(batches, s(
							"--set",
							getSketchybarWindowID(windowID),
							"display="+strconv.Itoa(monitor.Monitor),
						))

						item.windowIDs[windowID] = workspace.Workspace
					}
				} else {
					item.logger.Info("Adding new window to workspace",
						slog.Int("window.id", window.ID),
						slog.String("window.app", window.App),
						slog.String("workspace", workspace.Workspace),
					)

					var sketchybarWindowID string
					batches, sketchybarWindowID = item.addWindowToSketchybar(
						batches,
						item.position,
						isFocusedWorkspace,
						monitor.Monitor,
						workspace.Workspace,
						window.ID,
						window.App,
					)

					batches = batch(batches, s(
						"--move",
						sketchybarWindowID,
						"after",
						getSketchybarWorkspaceID(workspace.Workspace),
					))

					workspacesNeedingUpdate[workspace.Workspace] = true
				}
			}
		}
	}

	for windowID := range item.windowIDs {
		if _, found := tree.IndexedWindows[windowID]; !found {
			item.logger.Info("Removing window", slog.Int("windowID", windowID))
			oldWorkspaceID := item.windowIDs[windowID]
			batches = item.removeWindow(batches, windowID)
			workspacesNeedingUpdate[oldWorkspaceID] = true
		}
	}

	batches = item.updateWindowVisibility(ctx, batches, focusedWorkspaceID)

	for workspaceID := range workspacesNeedingUpdate {
		isFocused := workspaceID == focusedWorkspaceID
		batches = item.updateWorkspaceBracket(batches, workspaceID, isFocused)
	}

	return batches, aggregatedErr
}

func (item *AerospaceItem) updateWindowVisibility(
	ctx context.Context,
	batches Batches,
	focusedWorkspaceID string,
) Batches {
	tree := item.aerospace.GetTree()
	if tree == nil {
		return batches
	}

	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue
			}

			isFocusedWorkspace := workspace.Workspace == focusedWorkspaceID
			windowVisibility := item.getWindowVisibility(isFocusedWorkspace)

			for _, windowID := range workspace.Windows {
				sketchybarWindowID := getSketchybarWindowID(windowID)

				windowItem := &sketchybar.ItemOptions{
					Width: windowVisibility.width,
					Icon: sketchybar.ItemIconOptions{
						Drawing: windowVisibility.show,
					},
				}

				batches = batch(batches, m(
					s("--set", sketchybarWindowID),
					windowItem.ToArgs(),
				))
			}
		}
	}

	return batches
}

func (item *AerospaceItem) handleFrontAppSwitched(
	ctx context.Context,
	batches Batches,
	windowApp string,
) Batches {
	item.aerospace.SetFocusedApp(windowApp)
	item.aerospace.SingleFlightRefreshTree()
	focusedWorkspaceID := item.aerospace.GetFocusedWorkspaceID(ctx)
	batches = item.highlightWindows(batches, focusedWorkspaceID, windowApp)
	return batches
}

func (item *AerospaceItem) workspaceToSketchybar(
	isFocusedWorkspace bool,
	monitorsCount int,
	monitorID int,
	workspaceID string,
) (*sketchybar.ItemOptions, error) {
	icon, hasIcon := icons.Workspace[workspaceID]
	if !hasIcon {
		item.logger.Info(
			"could not find icon for app",
			slog.String("app", workspaceID),
		)
		return nil, fmt.Errorf("could not find icon for workspace %s", workspaceID)
	}

	colors := item.getWorkspaceColors(isFocusedWorkspace)

	return &sketchybar.ItemOptions{
		Display: item.getSketchybarDisplayIndex(monitorsCount, monitorID),
		Padding: sketchybar.PaddingOptions{
			Left:  pointer(0),
			Right: pointer(0),
		},
		Background: sketchybar.BackgroundOptions{
			Drawing: "on",
			Color: sketchybar.ColorOptions{
				Color: colors.backgroundColor,
			},
		},
		Icon: sketchybar.ItemIconOptions{
			Value: icon,
			Color: sketchybar.ColorOptions{
				Color: colors.color,
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.Aerospace.Padding,
				Right: settings.Sketchybar.Aerospace.Padding,
			},
		},
		ClickScript: fmt.Sprintf(`aerospace workspace "%s"`, workspaceID),
	}, nil
}

func (item *AerospaceItem) windowToSketchybar(
	isFocusedWorkspace bool,
	monitorID aerospace.MonitorID,
	workspaceID aerospace.WorkspaceID,
	windowApp string,
) *sketchybar.ItemOptions {
	iconInfo, hasIcon := icons.App[windowApp]
	if !hasIcon {
		item.logger.Info(
			"could not find icon for app",
			slog.String("app", windowApp),
		)
		iconInfo = icons.IconInfo{Icon: icons.Unknown, Font: settings.FontAppIcon}
	}

	windowVisibility := item.getWindowVisibility(isFocusedWorkspace)
	itemOptions := &sketchybar.ItemOptions{
		Display: strconv.Itoa(monitorID),
		Width:   windowVisibility.width,
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
		Icon: sketchybar.ItemIconOptions{
			Drawing: windowVisibility.show,
			Color: sketchybar.ColorOptions{
				Color: windowVisibility.color,
			},
			Font: sketchybar.FontOptions{
				Font: iconInfo.Font,
				Kind: "Regular",
				Size: "14.0",
			},
			Padding: sketchybar.PaddingOptions{
				Left:  settings.Sketchybar.Aerospace.Padding,
				Right: settings.Sketchybar.Aerospace.Padding,
			},
			Value: iconInfo.Icon,
		},
		ClickScript: fmt.Sprintf(`aerospace workspace "%s"`, workspaceID),
	}

	if utils.Equals(windowApp, item.aerospace.GetFocusedApp()) {
		itemOptions.Icon.Color = sketchybar.ColorOptions{
			Color: windowVisibility.focusedColor,
		}
	}

	return itemOptions
}

func getSketchybarWorkspaceID(spaceID aerospace.WorkspaceID) string {
	return fmt.Sprintf("%s.%s", workspaceItemPrefix, spaceID)
}

func getSketchybarWindowID(windowID aerospace.WindowID) string {
	return fmt.Sprintf("%s.%d", windowItemPrefix, windowID)
}

func getSketchybarBracketID(spaceID aerospace.WorkspaceID) string {
	return fmt.Sprintf("%s.%s", bracketItemPrefix, spaceID)
}

func getSketchybarSpacerID(spaceID aerospace.WorkspaceID) string {
	return fmt.Sprintf("%s.%s", spacerItemPrefix, spaceID)
}

func (item *AerospaceItem) removeWindow(batches Batches, windowID aerospace.WindowID) Batches {
	_, found := item.windowIDs[windowID]
	if found {
		delete(item.windowIDs, windowID)
		batches = batch(batches, s("--remove", getSketchybarWindowID(windowID)))
		item.logger.Debug("Removed window", slog.Int("windowID", windowID))
	}
	return batches
}

func (item *AerospaceItem) addWindowToSketchybar(
	batches Batches,
	position sketchybar.Position,
	isFocusedWorkspace bool,
	monitorID aerospace.MonitorID,
	workspaceID aerospace.WorkspaceID,
	windowID aerospace.WindowID,
	windowApp string,
) (Batches, string) {
	windowItem := item.windowToSketchybar(
		isFocusedWorkspace,
		monitorID,
		workspaceID,
		windowApp,
	)

	sketchybarWindowID := getSketchybarWindowID(windowID)

	item.windowIDs[windowID] = workspaceID

	batches = batch(batches, s("--add", "item", sketchybarWindowID, position))
	batches = batch(batches, m(s("--set", sketchybarWindowID), windowItem.ToArgs()))

	return batches, sketchybarWindowID
}

func checker(batches Batches, position sketchybar.Position) (Batches, error) {
	updateEvent, err := args.BuildEvent()
	if err != nil {
		return batches, errors.New("aerospace: could not generate update event")
	}

	checkerItem := sketchybar.ItemOptions{
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
		Updates: "on",
		Script:  updateEvent,
	}

	batches = batch(batches, s("--add", "item", aerospaceCheckerItemName, position))
	batches = batch(batches, m(s("--set", aerospaceCheckerItemName), checkerItem.ToArgs()))
	batches = batch(batches, s("--subscribe", aerospaceCheckerItemName,
		events.DisplayChange,
		events.SpaceWindowsChange,
		events.SystemWoke,
		events.FrontAppSwitched,
	))

	return batches, nil
}

type workspaceColors struct {
	backgroundColor string
	color           string
}

func (item *AerospaceItem) getWorkspaceColors(isFocusedWorkspace bool) workspaceColors {
	backgroundColor := settings.Sketchybar.Aerospace.WorkspaceBackgroundColor
	color := settings.Sketchybar.Aerospace.WorkspaceColor

	if isFocusedWorkspace {
		backgroundColor = settings.Sketchybar.Aerospace.WorkspaceFocusedBackgroundColor
		color = settings.Sketchybar.Aerospace.WorkspaceFocusedColor
	}

	return workspaceColors{
		backgroundColor,
		color,
	}
}

type windowVisibility struct {
	width        *int
	show         string
	color        string
	focusedColor string
}

func (item *AerospaceItem) getWindowVisibility(isFocusedWorkspace bool) *windowVisibility {
	width := pointer(32)
	show := "on"
	color := settings.Sketchybar.Aerospace.WindowColor
	focusedColor := settings.Sketchybar.Aerospace.WindowFocusedColor

	if !isFocusedWorkspace {
		width = pointer(0)
		show = "off"
		color = colorsPkg.Transparent
		focusedColor = colorsPkg.Transparent
	}

	return &windowVisibility{
		width,
		show,
		color,
		focusedColor,
	}
}

func (item *AerospaceItem) handleWorkspaceChange(
	ctx context.Context,
	batches Batches,
	prevWorkspaceID string,
	focusedWorkspaceID string,
) Batches {
	item.aerospace.SingleFlightRefreshTree()
	tree := item.aerospace.GetTree()

	if tree == nil {
		item.logger.Error("Tree is nil during workspace change")
		return batches
	}

	workspacesNeedingUpdate := make(map[aerospace.WorkspaceID]bool)

	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue
			}

			for _, windowID := range workspace.Windows {
				window := tree.IndexedWindows[windowID]
				if window == nil {
					continue
				}

				currentWorkspaceID, found := item.windowIDs[windowID]

				if found && currentWorkspaceID != workspace.Workspace {
					item.logger.Info("Moving window during workspace change",
						slog.Int("window.id", window.ID),
						slog.String("window.app", window.App),
						slog.String("old.workspace", currentWorkspaceID),
						slog.String("new.workspace", workspace.Workspace),
					)

					workspacesNeedingUpdate[currentWorkspaceID] = true
					workspacesNeedingUpdate[workspace.Workspace] = true

					batches = batch(batches, s(
						"--move",
						getSketchybarWindowID(windowID),
						"after",
						getSketchybarWorkspaceID(workspace.Workspace),
					))

					batches = batch(batches, s(
						"--set",
						getSketchybarWindowID(windowID),
						"display="+strconv.Itoa(monitor.Monitor),
					))

					item.windowIDs[windowID] = workspace.Workspace
				}
			}
		}
	}

	for workspaceID := range workspacesNeedingUpdate {
		isFocused := workspaceID == focusedWorkspaceID
		batches = item.updateWorkspaceBracket(batches, workspaceID, isFocused)
	}

	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue
			}

			isFocusedWorkspace := workspace.Workspace == focusedWorkspaceID

			sketchybarWorkspaceID := getSketchybarWorkspaceID(workspace.Workspace)
			colors := item.getWorkspaceColors(isFocusedWorkspace)
			workspaceItem := &sketchybar.ItemOptions{
				Background: sketchybar.BackgroundOptions{
					Color: sketchybar.ColorOptions{
						Color: colors.backgroundColor,
					},
				},
				Icon: sketchybar.ItemIconOptions{
					Color: sketchybar.ColorOptions{
						Color: colors.color,
					},
				},
			}

			batches = batch(batches, m(
				s(
					"--animate",
					sketchybar.AnimationTanh,
					settings.Sketchybar.Aerospace.TransitionTime,
					"--set",
					sketchybarWorkspaceID,
				),
				workspaceItem.ToArgs(),
			))

			sketchybarBracketID := getSketchybarBracketID(workspace.Workspace)
			bracketItem := sketchybar.BracketOptions{
				Background: sketchybar.BackgroundOptions{
					Border: sketchybar.BorderOptions{
						Color: colors.backgroundColor,
					},
				},
			}

			batches = batch(batches, m(
				s(
					"--animate",
					sketchybar.AnimationTanh,
					settings.Sketchybar.Aerospace.TransitionTime,
					"--set",
					sketchybarBracketID,
				),
				bracketItem.ToArgs(),
			))

			windowVisibility := item.getWindowVisibility(isFocusedWorkspace)
			for _, windowID := range workspace.Windows {
				window := tree.IndexedWindows[windowID]
				if window == nil {
					continue
				}

				sketchybarWindowID := getSketchybarWindowID(windowID)

				windowItem := item.windowToSketchybar(
					isFocusedWorkspace,
					monitor.Monitor,
					workspace.Workspace,
					window.App,
				)

				windowItem.Width = windowVisibility.width
				windowItem.Icon.Drawing = windowVisibility.show

				batches = batch(batches, m(
					s(
						"--animate",
						sketchybar.AnimationTanh,
						settings.Sketchybar.Aerospace.TransitionTime,
						"--set",
						sketchybarWindowID,
					),
					windowItem.ToArgs(),
				))
			}
		}
	}

	item.startDelayedPoll(ctx)

	return batches
}

func (item *AerospaceItem) getSketchybarDisplayIndex(
	monitorCount int,
	monitorID aerospace.MonitorID,
) string {
	if monitorCount == 0 {
		return "1"
	}

	result := monitorID + 1
	if result > monitorCount {
		result = 1
	}
	return strconv.Itoa(result)
}

func (item *AerospaceItem) handleDisplayChange(batches Batches) Batches {
	tree := item.aerospace.GetTree()
	if tree == nil {
		item.logger.Error("Tree is nil during display change")
		return batches
	}

	monitorCount := len(tree.Monitors)

	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue
			}

			displayIndex := item.getSketchybarDisplayIndex(monitorCount, monitor.Monitor)
			sketchybarWorkspaceID := getSketchybarWorkspaceID(workspace.Workspace)

			workspaceItem := &sketchybar.ItemOptions{
				Display: displayIndex,
			}

			batches = batch(batches, m(s("--set", sketchybarWorkspaceID), workspaceItem.ToArgs()))

			for _, windowID := range workspace.Windows {
				sketchybarWindowID := getSketchybarWindowID(windowID)

				windowItem := &sketchybar.ItemOptions{
					Display: displayIndex,
				}

				batches = batch(batches, m(s("--set", sketchybarWindowID), windowItem.ToArgs()))
			}
		}
	}

	focusedWorkspaceID := item.aerospace.GetFocusedWorkspaceID(context.Background())

	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue
			}
			isFocused := workspace.Workspace == focusedWorkspaceID
			batches = item.updateWorkspaceBracket(batches, workspace.Workspace, isFocused)
		}
	}

	return batches
}

func (item *AerospaceItem) addWorkspaceBracket(
	batches Batches,
	isFocusedWorkspace bool,
	workspaceID string,
	sketchybarWindowIDs []string,
) Batches {
	colors := item.getWorkspaceColors(isFocusedWorkspace)
	workspaceBracketItem := sketchybar.BracketOptions{
		Background: sketchybar.BackgroundOptions{
			Drawing: "on",
			Border: sketchybar.BorderOptions{
				Color: colors.backgroundColor,
			},
			Color: sketchybar.ColorOptions{
				Color: colorsPkg.Transparent,
			},
		},
	}

	sketchybarSpaceID := getSketchybarWorkspaceID(workspaceID)
	sketchybarBracketID := getSketchybarBracketID(workspaceID)

	batches = batch(batches, m(s(
		"--add",
		"bracket",
		sketchybarBracketID,
		sketchybarSpaceID),
		sketchybarWindowIDs,
	))

	batches = batch(batches, m(s(
		"--set",
		sketchybarBracketID,
	), workspaceBracketItem.ToArgs()))

	return batches
}

func (item *AerospaceItem) addWorkspaceSpacer(
	batches Batches,
	workspaceID string,
	position sketchybar.Position,
) Batches {
	workspaceSpacerItem := sketchybar.ItemOptions{
		Width: pointer(*settings.Sketchybar.ItemSpacing * 2),
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}

	sketchybarSpacerID := getSketchybarSpacerID(workspaceID)
	batches = batch(batches, s(
		"--add",
		"item",
		sketchybarSpacerID,
		position,
	))
	batches = batch(batches, m(s(
		"--set",
		sketchybarSpacerID,
	), workspaceSpacerItem.ToArgs()))

	return batches
}

func (item *AerospaceItem) applyTree(
	ctx context.Context,
	batches Batches,
	position sketchybar.Position,
) (Batches, error) {
	tree := item.aerospace.GetTree()
	if tree == nil {
		item.logger.Error("Tree is nil during applyTree")
		return batches, errors.New("aerospace tree is nil")
	}

	focusedSpaceID := item.aerospace.GetFocusedWorkspaceID(ctx)

	aerospaceSpacerItem := sketchybar.ItemOptions{
		Width: pointer(*settings.Sketchybar.ItemSpacing * 2),
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}

	sketchybarSpacerID := "aerospace.spacer"
	batches = batch(batches, s(
		"--add",
		"item",
		sketchybarSpacerID,
		position,
	))
	batches = batch(batches, m(s(
		"--set",
		sketchybarSpacerID,
	), aerospaceSpacerItem.ToArgs()))

	var aggregatedErr error
	for _, monitor := range tree.Monitors {
		item.logger.DebugContext(ctx, "monitor", slog.Int("monitor", monitor.Monitor))

		visibleWorkspaces := []*aerospace.WorkspaceWithWindowIDs{}
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; ok {
				visibleWorkspaces = append(visibleWorkspaces, workspace)
			}
		}

		for i, workspace := range visibleWorkspaces {
			isFocusedWorkspace := focusedSpaceID == workspace.Workspace

			item.logger.DebugContext(
				ctx,
				"workspace",
				slog.String("workspace", workspace.Workspace),
				slog.Bool("focused", isFocusedWorkspace),
			)

			sketchybarSpaceID := getSketchybarWorkspaceID(workspace.Workspace)
			workspaceSpace, err := item.workspaceToSketchybar(
				isFocusedWorkspace,
				len(tree.Monitors),
				monitor.Monitor,
				workspace.Workspace,
			)
			if err != nil {
				aggregatedErr = errors.Join(aggregatedErr, err)
				continue
			}

			batches = batch(batches, s("--add", "item", sketchybarSpaceID, position))
			batches = batch(batches, m(s("--set", sketchybarSpaceID), workspaceSpace.ToArgs()))

			sketchybarWindowIDs := make([]string, len(workspace.Windows))
			for i, windowID := range workspace.Windows {
				window := tree.IndexedWindows[windowID]
				if window == nil {
					item.logger.Warn("Window not found in tree during apply", slog.Int("windowID", windowID))
					continue
				}

				item.logger.DebugContext(
					ctx,
					"window",
					slog.Int("window", window.ID),
					slog.String("app", window.App),
				)

				var sketchybarWindowID string
				batches, sketchybarWindowID = item.addWindowToSketchybar(
					batches,
					position,
					isFocusedWorkspace,
					monitor.Monitor,
					workspace.Workspace,
					window.ID,
					window.App,
				)
				sketchybarWindowIDs[i] = sketchybarWindowID
			}

			if len(workspace.Windows) > 0 {
				batches = item.addWorkspaceBracket(batches, isFocusedWorkspace, workspace.Workspace, sketchybarWindowIDs)
			}

			if i < len(visibleWorkspaces)-1 {
				batches = item.addWorkspaceSpacer(batches, workspace.Workspace, position)
			}
		}
	}

	return batches, aggregatedErr
}

func (item *AerospaceItem) highlightWindows(
	batches Batches,
	workspaceID string,
	app string,
) Batches {
	windowsOfFocusedWorkspace := item.aerospace.WindowsOfWorkspace(workspaceID)

	for _, window := range windowsOfFocusedWorkspace {
		windowItemID := getSketchybarWindowID(window.ID)

		color := settings.Sketchybar.Aerospace.WindowColor
		if utils.Equals(window.App, app) {
			color = settings.Sketchybar.Aerospace.WindowFocusedColor
		}

		windowItem := sketchybar.ItemOptions{
			Icon: sketchybar.ItemIconOptions{
				Color: sketchybar.ColorOptions{
					Color: color,
				},
			},
		}

		batches = batch(batches, m(s(
			"--animate",
			sketchybar.AnimationTanh,
			settings.Sketchybar.Aerospace.TransitionTime,
			"--set",
			windowItemID,
		), windowItem.ToArgs()))
	}

	return batches
}

func isAerospace(name string) bool {
	return name == AerospaceName
}

var _ WentsketchyItem = (*AerospaceItem)(nil)