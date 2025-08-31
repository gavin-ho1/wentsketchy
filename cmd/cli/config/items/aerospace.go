package items

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
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
	logger              *slog.Logger
	aerospace           aerospace.Aerospace
	sketchybar          sketchybar.API
	position            sketchybar.Position
	renderedItems       map[string]bool
	closingItems        map[string]time.Time // Track items being closed for delayed removal
	workspaceWindowIDs  map[string][]string  // Track window IDs for each workspace
	pendingBracketUpdates map[string][]string // Track brackets that need updating after animations
	bracketStates       map[string]string    // Track bracket creation state to prevent duplicates
}

func NewAerospaceItem(
	logger *slog.Logger,
	aerospace aerospace.Aerospace,
	sketchybarAPI sketchybar.API,
) *AerospaceItem {
	return &AerospaceItem{
		logger:                logger,
		aerospace:             aerospace,
		sketchybar:            sketchybarAPI,
		position:              sketchybar.PositionLeft,
		renderedItems:         make(map[string]bool),
		closingItems:          make(map[string]time.Time),
		workspaceWindowIDs:    make(map[string][]string),
		pendingBracketUpdates: make(map[string][]string),
		bracketStates:         make(map[string]string),
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
	
	// First, remove brackets for workspaces that no longer have windows
	for workspaceID := range item.workspaceWindowIDs {
		bracketID := getSketchybarBracketID(workspaceID)
		if item.renderedItems[bracketID] && !newItems[bracketID] {
			// Workspace no longer has windows, remove bracket immediately
			batches = batch(batches, s("--remove", bracketID))
			delete(item.bracketStates, workspaceID)
		}
	}
	
	for itemID := range item.renderedItems {
		if !newItems[itemID] {
			// Start closing animation if not already started
			if _, isClosing := item.closingItems[itemID]; !isClosing {
				item.closingItems[itemID] = now
				
				// For brackets, remove immediately to prevent glitches
				if isBracketItem(itemID) {
					batches = batch(batches, s("--remove", itemID))
					workspaceFromBracket := extractWorkspaceFromBracketID(itemID)
					delete(item.bracketStates, workspaceFromBracket)
					delete(item.closingItems, itemID) // Don't need to track bracket closing
					continue
				}
				
				// Animate windows to closed state
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
					// For new windows, ensure proper positioning before animation
					batches = batch(batches, s("--move", sketchybarWindowID, "after", prevSketchybarItemID))
				}
				
				// Animate to final state (opening animation for new windows, update for existing)
				batches = batch(batches, m(
					s("--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime, "--set", sketchybarWindowID),
					windowItem.ToArgs(),
				))
				
				prevSketchybarItemID = sketchybarWindowID
			}

			// Handle brackets with improved logic to prevent glitches
			if len(workspace.Windows) > 0 {
				batches = item.handleWorkspaceBracket(batches, workspace, sketchybarWindowIDs, isFocusedWorkspace, transitionDuration, now)
			} else {
				// Clear tracking and remove bracket when no windows
				item.cleanupWorkspaceBracket(workspace.Workspace)
			}

			if i < len(visibleWorkspaces)-1 {
				spacerID := getSketchybarSpacerID(workspace.Workspace)
				if !item.renderedItems[spacerID] {
					batches = item.addWorkspaceSpacer(batches, workspace.Workspace, position)
				}
			}
		}
	}

	// --- 4. Process pending bracket updates with better timing ---
	batches = item.processPendingBracketUpdates(batches, focusedWorkspaceID, transitionDuration, now, tree)

	item.renderedItems = newItems
	return batches, aggregatedErr
}

// Improved bracket handling to prevent glitches
func (item *AerospaceItem) handleWorkspaceBracket(
	batches Batches,
	workspace *aerospace.WorkspaceWithWindowIDs,
	sketchybarWindowIDs []string,
	isFocusedWorkspace bool,
	transitionDuration time.Duration,
	now time.Time,
) Batches {
	sketchybarBracketID := getSketchybarBracketID(workspace.Workspace)
	
	// Check if we need to update the bracket
	needsUpdate := !item.renderedItems[sketchybarBracketID] || item.bracketNeedsUpdate(workspace.Workspace, sketchybarWindowIDs)
	
	if !needsUpdate {
		// Just update bracket colors without recreating
		colors := item.getWorkspaceColors(isFocusedWorkspace)
		if item.renderedItems[sketchybarBracketID] {
			batches = batch(batches, s(
				"--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime,
				"--set", sketchybarBracketID,
				fmt.Sprintf("background.border_color=%s", colors.backgroundColor),
			))
		}
		return batches
	}
	
	// Check if any windows are currently animating
	hasAnimatingWindows := item.hasAnimatingWindows(workspace.Workspace, sketchybarWindowIDs, now, transitionDuration)
	
	if hasAnimatingWindows {
		// Defer bracket update until animations complete
		item.pendingBracketUpdates[workspace.Workspace] = sketchybarWindowIDs
		item.logger.Debug("Deferring bracket update due to animations", slog.String("workspace", workspace.Workspace))
	} else {
		// Safe to update bracket immediately
		batches = item.updateWorkspaceBracket(batches, workspace.Workspace, sketchybarWindowIDs, isFocusedWorkspace)
	}
	
	return batches
}

// Check if workspace has any animating windows
func (item *AerospaceItem) hasAnimatingWindows(workspaceID string, currentWindowIDs []string, now time.Time, transitionDuration time.Duration) bool {
	// Check for opening animations (new windows)
	for _, windowID := range currentWindowIDs {
		if !item.renderedItems[windowID] {
			return true
		}
	}
	
	// Check for closing animations in this workspace
	for closingItemID, closingStartTime := range item.closingItems {
		if isWindowItem(closingItemID) && belongsToWorkspace(closingItemID, workspaceID) {
			if now.Sub(closingStartTime) < transitionDuration {
				return true
			}
		}
	}
	
	return false
}

// Update workspace bracket with improved error handling
func (item *AerospaceItem) updateWorkspaceBracket(batches Batches, workspaceID string, windowIDs []string, isFocused bool) Batches {
	sketchybarBracketID := getSketchybarBracketID(workspaceID)
	
	// Always remove existing bracket first to prevent conflicts
	if item.renderedItems[sketchybarBracketID] {
		batches = batch(batches, s("--remove", sketchybarBracketID))
		item.logger.Debug("Removed existing bracket", slog.String("bracketID", sketchybarBracketID))
	}
	
	// Clear any stale state
	delete(item.bracketStates, workspaceID)
	delete(item.pendingBracketUpdates, workspaceID)
	
	// Add new bracket
	batches = item.addWorkspaceBracket(batches, isFocused, workspaceID, windowIDs)
	item.workspaceWindowIDs[workspaceID] = windowIDs
	
	// Track bracket state
	bracketsKey := strings.Join(windowIDs, ",")
	item.bracketStates[workspaceID] = bracketsKey
	
	item.logger.Debug("Updated workspace bracket", 
		slog.String("workspace", workspaceID),
		slog.Int("windowCount", len(windowIDs)))
	
	return batches
}

// Process pending bracket updates with improved timing
func (item *AerospaceItem) processPendingBracketUpdates(
	batches Batches,
	focusedWorkspaceID string,
	transitionDuration time.Duration,
	now time.Time,
	tree *aerospace.Tree,
) Batches {
	for workspaceID, windowIDs := range item.pendingBracketUpdates {
		// Check if all animations for this workspace have completed
		if !item.hasAnimatingWindows(workspaceID, windowIDs, now, transitionDuration) {
			// All animations complete, safe to update bracket
			isFocused := focusedWorkspaceID == workspaceID
			batches = item.updateWorkspaceBracket(batches, workspaceID, windowIDs, isFocused)
			
			item.logger.Debug("Processed pending bracket update", slog.String("workspace", workspaceID))
		}
	}
	
	return batches
}

// Clean up workspace bracket state
func (item *AerospaceItem) cleanupWorkspaceBracket(workspaceID string) {
	delete(item.workspaceWindowIDs, workspaceID)
	delete(item.pendingBracketUpdates, workspaceID)
	delete(item.bracketStates, workspaceID)
}

// Helper function to determine if bracket needs updating
func (item *AerospaceItem) bracketNeedsUpdate(workspaceID string, currentWindowIDs []string) bool {
	previousWindowIDs, exists := item.workspaceWindowIDs[workspaceID]
	if !exists {
		// No previous state, needs update if we have windows
		return len(currentWindowIDs) > 0
	}

	// Check if the window composition has changed
	if len(previousWindowIDs) != len(currentWindowIDs) {
		return true
	}

	// Check if the actual window IDs have changed (order matters for brackets)
	for i, windowID := range currentWindowIDs {
		if i >= len(previousWindowIDs) || windowID != previousWindowIDs[i] {
			return true
		}
	}

	return false
}

// Helper function to check if an item ID represents a window
func isWindowItem(itemID string) bool {
	return len(itemID) > len(windowItemPrefix) && itemID[:len(windowItemPrefix)] == windowItemPrefix
}

// Helper function to check if an item ID represents a bracket
func isBracketItem(itemID string) bool {
	return len(itemID) > len(bracketItemPrefix) && itemID[:len(bracketItemPrefix)] == bracketItemPrefix
}

// Extract workspace ID from bracket item ID
func extractWorkspaceFromBracketID(bracketItemID string) string {
	if !isBracketItem(bracketItemID) {
		return ""
	}
	return bracketItemID[len(bracketItemPrefix)+1:] // +1 for the dot
}

// Helper function to check if a window item belongs to a specific workspace
func belongsToWorkspace(windowItemID, workspaceID string) bool {
	// In the current implementation, we can't directly determine workspace from window ID
	// This is a limitation that could be improved by encoding workspace info in the window ID
	// For now, we use the animation timing approach
	return true
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

	// Ensure we have valid window IDs and they're properly positioned
	validWindowIDs := []string{}
	for _, windowID := range sketchybarWindowIDs {
		if windowID != "" {
			validWindowIDs = append(validWindowIDs, windowID)
		}
	}

	if len(validWindowIDs) == 0 {
		item.logger.Warn("No valid window IDs for bracket", slog.String("workspace", workspaceID))
		return batches
	}

	item.logger.Debug("Adding workspace bracket", 
		slog.String("workspace", workspaceID),
		slog.String("bracketID", sketchybarBracketID),
		slog.Any("windowIDs", validWindowIDs))

	batches = batch(batches, m(s(
		"--add",
		"bracket",
		sketchybarBracketID,
		sketchybarSpaceID),
		validWindowIDs,
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