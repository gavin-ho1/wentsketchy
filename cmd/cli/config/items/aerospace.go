package items

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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
	logger        *slog.Logger
	aerospace     aerospace.Aerospace
	sketchybar    sketchybar.API
	position      sketchybar.Position
	renderedItems map[string]bool
	closingItems  map[string]time.Time // Track items being closed for delayed removal
}

func NewAerospaceItem(
	logger *slog.Logger,
	aerospace aerospace.Aerospace,
	sketchybarAPI sketchybar.API,
) *AerospaceItem {
	return &AerospaceItem{
		logger:        logger,
		aerospace:     aerospace,
		sketchybar:    sketchybarAPI,
		position:      sketchybar.PositionLeft,
		renderedItems: make(map[string]bool),
		closingItems:  make(map[string]time.Time),
	}
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
	return item.render(ctx, batches, position)
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
	case events.FrontAppSwitched:
		item.aerospace.SetFocusedApp(args.Info)
	}

	return item.render(ctx, batches, position)
}

func (item *AerospaceItem) render(
	ctx context.Context,
	batches Batches,
	position sketchybar.Position,
) (Batches, error) {
	item.aerospace.SingleFlightRefreshTree()
	tree := item.aerospace.GetTree()
	if tree == nil {
		item.logger.Error("Tree is nil during render")
		return batches, errors.New("aerospace tree is nil")
	}

	focusedWorkspaceID := item.aerospace.GetFocusedWorkspaceID(ctx)
	newItems := make(map[string]bool)

	// --- 1. Determine all items that should be on the bar ---
	newItems[aerospaceCheckerItemName] = true
	newItems["aerospace.spacer"] = true

	for _, monitor := range tree.Monitors {
		visibleWorkspaces := []*aerospace.WorkspaceWithWindowIDs{}
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; ok {
				visibleWorkspaces = append(visibleWorkspaces, workspace)
			}
		}

		for i, workspace := range visibleWorkspaces {
			newItems[getSketchybarWorkspaceID(workspace.Workspace)] = true
			for _, windowID := range workspace.Windows {
				newItems[getSketchybarWindowID(windowID)] = true
			}
			if len(workspace.Windows) > 0 {
				newItems[getSketchybarBracketID(workspace.Workspace)] = true
			}
			if i < len(visibleWorkspaces)-1 {
				newItems[getSketchybarSpacerID(workspace.Workspace)] = true
			}
		}
	}

	// --- 2. Handle closing animations and cleanup expired items ---
	now := time.Now()
	transitionTimeMs, err := strconv.Atoi(settings.Sketchybar.Aerospace.TransitionTime)
	if err != nil {
		item.logger.Error("could not parse TransitionTime, using default", slog.Any("error", err))
		transitionTimeMs = 5
	}
	transitionDuration := time.Duration(transitionTimeMs) * time.Millisecond
	
	for itemID := range item.renderedItems {
		if !newItems[itemID] {
			// Start closing animation if not already started
			if _, isClosing := item.closingItems[itemID]; !isClosing {
				item.closingItems[itemID] = now
				// Animate to closed state (reverse animation)
				if isWindowItem(itemID) {
					batches = batch(batches, s(
						"--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime,
						"--set", itemID,
						"icon.drawing=off",
						"width=0",
					))
				}
			}
		}
	}

	// Remove items that have finished their closing animation
	for itemID, closingStartTime := range item.closingItems {
		if now.Sub(closingStartTime) >= transitionDuration {
			batches = batch(batches, s("--remove", itemID))
			delete(item.closingItems, itemID)
		}
	}

	// --- 3. Add or update items ---
	var aggregatedErr error

	// Checker
	if !item.renderedItems[aerospaceCheckerItemName] {
		var err error
		batches, err = checker(batches, position)
		if err != nil {
			aggregatedErr = errors.Join(aggregatedErr, err)
		}
	}

	// Spacer
	aerospaceSpacerItem := sketchybar.ItemOptions{
		Width:      pointer(*settings.Sketchybar.ItemSpacing * 2),
		Background: sketchybar.BackgroundOptions{Drawing: "off"},
	}
	sketchybarSpacerID := "aerospace.spacer"
	if !item.renderedItems[sketchybarSpacerID] {
		batches = batch(batches, s("--add", "item", sketchybarSpacerID, position))
	}
	batches = batch(batches, m(s("--set", sketchybarSpacerID), aerospaceSpacerItem.ToArgs()))

	// Workspaces, windows, brackets, spacers
	for _, monitor := range tree.Monitors {
		visibleWorkspaces := []*aerospace.WorkspaceWithWindowIDs{}
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; ok {
				visibleWorkspaces = append(visibleWorkspaces, workspace)
			}
		}

		for i, workspace := range visibleWorkspaces {
			isFocusedWorkspace := focusedWorkspaceID == workspace.Workspace
			sketchybarSpaceID := getSketchybarWorkspaceID(workspace.Workspace)
			workspaceSpace, err := item.workspaceToSketchybar(
				isFocusedWorkspace, len(tree.Monitors), monitor.Monitor, workspace.Workspace,
			)
			if err != nil {
				aggregatedErr = errors.Join(aggregatedErr, err)
				continue
			}

			if !item.renderedItems[sketchybarSpaceID] {
				batches = batch(batches, s("--add", "item", sketchybarSpaceID, position))
			}
			batches = batch(batches, m(
				s("--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime, "--set", sketchybarSpaceID),
				workspaceSpace.ToArgs(),
			))

			sketchybarWindowIDs := make([]string, len(workspace.Windows))
			
			// First pass: Add all windows and determine positioning
			for j, windowID := range workspace.Windows {
				window := tree.IndexedWindows[windowID]
				if window == nil {
					continue
				}
				windowItem := item.windowToSketchybar(isFocusedWorkspace, monitor.Monitor, workspace.Workspace, window.App)
				sketchybarWindowID := getSketchybarWindowID(windowID)
				
				isNewWindow := !item.renderedItems[sketchybarWindowID]
				if isNewWindow {
					// Add window with initial closed state for opening animation
					initialWindowItem := *windowItem
					initialWindowItem.Width = pointer(0)
					initialWindowItem.Icon.Drawing = "off"
					
					batches = batch(batches, s("--add", "item", sketchybarWindowID, position))
					batches = batch(batches, m(s("--set", sketchybarWindowID), initialWindowItem.ToArgs()))
				}
				
				sketchybarWindowIDs[j] = sketchybarWindowID
			}
			
			// Second pass: Position windows and animate
			prevSketchybarItemID := sketchybarSpaceID // Start positioning after the workspace icon
			for _, windowID := range workspace.Windows {
				window := tree.IndexedWindows[windowID]
				if window == nil {
					continue
				}
				windowItem := item.windowToSketchybar(isFocusedWorkspace, monitor.Monitor, workspace.Workspace, window.App)
				sketchybarWindowID := getSketchybarWindowID(windowID)
				
				isNewWindow := !item.renderedItems[sketchybarWindowID]
				
				// Position window
				batches = batch(batches, s("--move", sketchybarWindowID, "after", prevSketchybarItemID))
				
				if isNewWindow {
					// For new windows, position after the rightmost existing window first, then animate
					// Find the rightmost existing window in this workspace
					rightmostExisting := sketchybarSpaceID
					for k := len(workspace.Windows) - 1; k >= 0; k-- {
						checkWindowID := getSketchybarWindowID(workspace.Windows[k])
						if item.renderedItems[checkWindowID] && checkWindowID != sketchybarWindowID {
							rightmostExisting = checkWindowID
							break
						}
					}
					batches = batch(batches, s("--move", sketchybarWindowID, "after", rightmostExisting))
				}
				
				// Animate to final state (opening animation for new windows, update for existing)
				batches = batch(batches, m(
					s("--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime, "--set", sketchybarWindowID),
					windowItem.ToArgs(),
				))
				
				prevSketchybarItemID = sketchybarWindowID
			}

			// Handle brackets more carefully to prevent flickering
			if len(workspace.Windows) > 0 {
				sketchybarBracketID := getSketchybarBracketID(workspace.Workspace)
				
				// Only recreate bracket if it doesn't exist or window composition changed
				if !item.renderedItems[sketchybarBracketID] || item.bracketNeedsUpdate(workspace.Workspace, sketchybarWindowIDs) {
					// Remove existing bracket if it exists
					if item.renderedItems[sketchybarBracketID] {
						batches = batch(batches, s("--remove", sketchybarBracketID))
					}
					batches = item.addWorkspaceBracket(batches, isFocusedWorkspace, workspace.Workspace, sketchybarWindowIDs)
				} else {
					// Just update bracket colors without recreating
					colors := item.getWorkspaceColors(isFocusedWorkspace)
					batches = batch(batches, s(
						"--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime,
						"--set", sketchybarBracketID,
						fmt.Sprintf("background.border_color=%s", colors.backgroundColor),
					))
				}
			}

			if i < len(visibleWorkspaces)-1 {
				spacerID := getSketchybarSpacerID(workspace.Workspace)
				if !item.renderedItems[spacerID] {
					batches = item.addWorkspaceSpacer(batches, workspace.Workspace, position)
				}
			}
		}
	}

	item.renderedItems = newItems
	return batches, aggregatedErr
}

// Helper function to determine if bracket needs updating
func (item *AerospaceItem) bracketNeedsUpdate(workspaceID string, currentWindowIDs []string) bool {
	// This is a simplified check - in a full implementation, you'd want to track
	// the previous window composition for each workspace
	return false // For now, assume brackets don't need recreation unless they don't exist
}

// Helper function to check if an item ID represents a window
func isWindowItem(itemID string) bool {
	return len(itemID) > len(windowItemPrefix) && itemID[:len(windowItemPrefix)] == windowItemPrefix
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

func isAerospace(name string) bool {
	return name == AerospaceName
}

var _ WentsketchyItem = (*AerospaceItem)(nil)