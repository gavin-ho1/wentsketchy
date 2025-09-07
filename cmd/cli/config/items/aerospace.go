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
	logger              *slog.Logger
	aerospace           aerospace.Aerospace
	sketchybar          sketchybar.API
	position            sketchybar.Position
	renderedItems       map[string]bool
	closingItems        map[string]time.Time // Track items being closed for delayed removal
	workspaceWindowIDs  map[string][]string  // Track window IDs for each workspace
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
		bracketStates:         make(map[string]string),
	}
}

const aerospaceCheckerItemName = "aerospace.checker"
const workspaceItemPrefix = "aerospace.workspace"
const windowItemPrefix = "aerospace.window"
const bracketItemPrefix = "aerospace.bracket"
const bracketSpacerItemPrefix = "aerospace.bracket.spacer"
const spacerItemPrefix = "aerospace.spacer"

const AerospaceName = aerospaceCheckerItemName

func (item *AerospaceItem) Init(
	ctx context.Context,
	position sketchybar.Position,
	batches Batches,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in Init", slog.Any("panic", r))
		}
	}()

	item.position = position
	
	result, err := item.renderSafely(ctx, batches, position)
	if err != nil {
		item.logger.ErrorContext(ctx, "aerospace item: Init failed, using fallback", slog.Any("error", err))
		// Return a minimal fallback instead of failing completely
		return item.createFallbackBatches(batches, position), nil
	}
	
	return result, nil
}

func (item *AerospaceItem) Update(
	ctx context.Context,
	batches Batches,
	position sketchybar.Position,
	args *args.In,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in Update", 
				slog.Any("panic", r),
				slog.String("event", args.Event))
		}
	}()

	item.position = position

	if !isAerospace(args.Name) {
		return batches, nil
	}

	// Handle events with error recovery
	if err := item.handleEventSafely(ctx, args); err != nil {
		item.logger.ErrorContext(ctx, "aerospace item: failed to handle event", 
			slog.Any("error", err),
			slog.String("event", args.Event))
		// Continue with render even if event handling fails
	}

	result, err := item.renderSafely(ctx, batches, position)
	if err != nil {
		item.logger.ErrorContext(ctx, "aerospace item: Update failed, using previous state", slog.Any("error", err))
		// Return current batches instead of failing
		return batches, nil
	}

	return result, nil
}

func (item *AerospaceItem) handleEventSafely(ctx context.Context, args *args.In) error {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in handleEventSafely", slog.Any("panic", r))
		}
	}()

	switch args.Event {
	case aerospace_events.WorkspaceChange:
		var data aerospace_events.WorkspaceChangeEventInfo
		if err := json.Unmarshal([]byte(args.Info), &data); err != nil {
			return fmt.Errorf("aerospace: could not deserialize json for workspace-change: %w", err)
		}
		item.aerospace.SetFocusedWorkspaceID(data.Focused)
		
	case events.FrontAppSwitched:
		item.aerospace.SetFocusedApp(args.Info)
		
	case aerospace_events.AerospaceRefresh:
		// No data to parse, just re-render
	}

	return nil
}

func (item *AerospaceItem) renderSafely(
	ctx context.Context,
	batches Batches,
	position sketchybar.Position,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in renderSafely", slog.Any("panic", r))
		}
	}()

	// Safely refresh aerospace tree
	func() {
		defer func() {
			if r := recover(); r != nil {
				item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in SingleFlightRefreshTree", slog.Any("panic", r))
			}
		}()
		item.aerospace.SingleFlightRefreshTree()
	}()

	tree := item.aerospace.GetTree()
	if tree == nil {
		item.logger.WarnContext(ctx, "Tree is nil during render, using fallback")
		return item.createFallbackBatches(batches, position), nil
	}

	// Get focused workspace safely
	focusedWorkspaceID := ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				item.logger.ErrorContext(ctx, "aerospace item: recovered from panic getting focused workspace", slog.Any("panic", r))
			}
		}()
		focusedWorkspaceID = item.aerospace.GetFocusedWorkspaceID(ctx)
	}()

	// Continue with render logic but with more error handling
	return item.renderWithErrorRecovery(ctx, batches, position, tree, focusedWorkspaceID)
}

func (item *AerospaceItem) renderWithErrorRecovery(
	ctx context.Context,
	batches Batches,
	position sketchybar.Position,
	tree *aerospace.Tree,
	focusedWorkspaceID string,
) (Batches, error) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in renderWithErrorRecovery", slog.Any("panic", r))
		}
	}()

	newItems := make(map[string]bool)
	var aggregatedErr error

	// Safely determine items that should be on the bar
	func() {
		defer func() {
			if r := recover(); r != nil {
				item.logger.ErrorContext(ctx, "aerospace item: recovered from panic determining new items", slog.Any("panic", r))
			}
		}()

		newItems[aerospaceCheckerItemName] = true
		newItems["aerospace.spacer"] = true

		for _, monitor := range tree.Monitors {
			if monitor == nil {
				continue
			}
			
			visibleWorkspaces := []*aerospace.WorkspaceWithWindowIDs{}
			for _, workspace := range monitor.Workspaces {
				if workspace == nil {
					continue
				}
				if _, ok := icons.Workspace[workspace.Workspace]; ok {
					visibleWorkspaces = append(visibleWorkspaces, workspace)
				}
			}

			for i, workspace := range visibleWorkspaces {
				if workspace == nil {
					continue
				}
				
				newItems[getSketchybarWorkspaceID(workspace.Workspace)] = true
				newItems[getSketchybarBracketID(workspace.Workspace)] = true
				newItems[getSketchybarBracketSpacerID(workspace.Workspace)] = true
				
				for _, windowID := range workspace.Windows {
					newItems[getSketchybarWindowID(windowID)] = true
				}
				
				if i < len(visibleWorkspaces)-1 {
					newItems[getSketchybarSpacerID(workspace.Workspace)] = true
				}
			}
		}
	}()

	// Safely handle closing animations and cleanup
	func() {
		defer func() {
			if r := recover(); r != nil {
				item.logger.ErrorContext(ctx, "aerospace item: recovered from panic during cleanup", slog.Any("panic", r))
			}
		}()

		now := time.Now()
		transitionTimeMs, err := strconv.Atoi(settings.Sketchybar.Aerospace.TransitionTime)
		if err != nil {
			item.logger.ErrorContext(ctx, "could not parse TransitionTime, using default", slog.Any("error", err))
			transitionTimeMs = 5
		}
		transitionDuration := time.Duration(transitionTimeMs) * time.Millisecond

		// Clean up brackets for workspaces that no longer have windows
		for workspaceID := range item.workspaceWindowIDs {
			bracketID := getSketchybarBracketID(workspaceID)
			if item.renderedItems[bracketID] && !newItems[bracketID] {
				batches = batch(batches, s("--remove", bracketID))
				delete(item.bracketStates, workspaceID)
			}
		}

		// Handle closing items
		for itemID := range item.renderedItems {
			if !newItems[itemID] {
				if _, isClosing := item.closingItems[itemID]; !isClosing {
					item.closingItems[itemID] = now

					if isBracketItem(itemID) {
						batches = batch(batches, s("--remove", itemID))
						workspaceFromBracket := extractWorkspaceFromBracketID(itemID)
						delete(item.bracketStates, workspaceFromBracket)
						delete(item.closingItems, itemID)
						continue
					}

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
	}()

	// Safely add checker and spacer
	func() {
		defer func() {
			if r := recover(); r != nil {
				item.logger.ErrorContext(ctx, "aerospace item: recovered from panic adding checker/spacer", slog.Any("panic", r))
			}
		}()

		if !item.renderedItems[aerospaceCheckerItemName] {
			var err error
			batches, err = checker(batches, position)
			if err != nil {
				item.logger.ErrorContext(ctx, "aerospace item: failed to create checker", slog.Any("error", err))
				aggregatedErr = errors.Join(aggregatedErr, err)
			}
		}

		// Add spacer
		aerospaceSpacerItem := sketchybar.ItemOptions{
			Width:      pointer(*settings.Sketchybar.ItemSpacing * 2),
			Background: sketchybar.BackgroundOptions{Drawing: "off"},
		}
		sketchybarSpacerID := "aerospace.spacer"
		if !item.renderedItems[sketchybarSpacerID] {
			batches = batch(batches, s("--add", "item", sketchybarSpacerID, position))
		}
		batches = batch(batches, m(s("--set", sketchybarSpacerID), aerospaceSpacerItem.ToArgs()))
	}()

	// Safely render workspaces and windows
	func() {
		defer func() {
			if r := recover(); r != nil {
				item.logger.ErrorContext(ctx, "aerospace item: recovered from panic rendering workspaces", slog.Any("panic", r))
			}
		}()

		for _, monitor := range tree.Monitors {
			if monitor == nil {
				continue
			}
			
			item.renderMonitorSafely(ctx, &batches, &aggregatedErr, monitor, tree, focusedWorkspaceID, position)
		}
	}()

	item.renderedItems = newItems
	return batches, aggregatedErr
}

func (item *AerospaceItem) renderMonitorSafely(
	ctx context.Context,
	batches *Batches,
	aggregatedErr *error,
	monitor *aerospace.Branch,
	tree *aerospace.Tree,
	focusedWorkspaceID string,
	position sketchybar.Position,
) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in renderMonitorSafely", slog.Any("panic", r))
		}
	}()

	visibleWorkspaces := []*aerospace.WorkspaceWithWindowIDs{}
	for _, workspace := range monitor.Workspaces {
		if workspace == nil {
			continue
		}
		if _, ok := icons.Workspace[workspace.Workspace]; ok {
			visibleWorkspaces = append(visibleWorkspaces, workspace)
		}
	}

	for i, workspace := range visibleWorkspaces {
		if workspace == nil {
			continue
		}
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					item.logger.ErrorContext(ctx, "aerospace item: recovered from panic rendering workspace", 
						slog.Any("panic", r),
						slog.String("workspace", workspace.Workspace))
				}
			}()
			
			item.renderWorkspaceSafely(ctx, batches, aggregatedErr, workspace, tree, focusedWorkspaceID, position, len(tree.Monitors), monitor.Monitor)
		}()

		// Add spacer between workspaces
		if i < len(visibleWorkspaces)-1 {
			spacerID := getSketchybarSpacerID(workspace.Workspace)
			if !item.renderedItems[spacerID] {
				*batches = item.addWorkspaceSpacer(*batches, workspace.Workspace, position)
			}
		}
	}
}

func (item *AerospaceItem) renderWorkspaceSafely(
	ctx context.Context,
	batches *Batches,
	aggregatedErr *error,
	workspace *aerospace.WorkspaceWithWindowIDs,
	tree *aerospace.Tree,
	focusedWorkspaceID string,
	position sketchybar.Position,
	monitorsCount int,
	monitorID int,
) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in renderWorkspaceSafely", slog.Any("panic", r))
		}
	}()

	isFocusedWorkspace := focusedWorkspaceID == workspace.Workspace
	sketchybarSpaceID := getSketchybarWorkspaceID(workspace.Workspace)
	
	// Render workspace icon safely
	workspaceSpace, err := item.workspaceToSketchybar(isFocusedWorkspace, monitorsCount, monitorID, workspace.Workspace)
	if err != nil {
		item.logger.ErrorContext(ctx, "aerospace item: failed to create workspace item", 
			slog.Any("error", err),
			slog.String("workspace", workspace.Workspace))
		*aggregatedErr = errors.Join(*aggregatedErr, err)
		return
	}

	if !item.renderedItems[sketchybarSpaceID] {
		*batches = batch(*batches, s("--add", "item", sketchybarSpaceID, position))
	}
	*batches = batch(*batches, m(
		s("--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime, "--set", sketchybarSpaceID),
		workspaceSpace.ToArgs(),
	))

	// Render windows safely
	item.renderWindowsSafely(ctx, batches, workspace, tree, isFocusedWorkspace, monitorID, position, sketchybarSpaceID)

	// Handle brackets and spacers safely
	item.handleBracketsAndSpacersSafely(ctx, batches, workspace, isFocusedWorkspace, position)
}

func (item *AerospaceItem) renderWindowsSafely(
	ctx context.Context,
	batches *Batches,
	workspace *aerospace.WorkspaceWithWindowIDs,
	tree *aerospace.Tree,
	isFocusedWorkspace bool,
	monitorID int,
	position sketchybar.Position,
	prevSketchybarItemID string,
) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in renderWindowsSafely", slog.Any("panic", r))
		}
	}()

	for _, windowID := range workspace.Windows {
		window := tree.IndexedWindows[windowID]
		if window == nil {
			continue
		}
		
		func() {
			defer func() {
				if r := recover(); r != nil {
					item.logger.ErrorContext(ctx, "aerospace item: recovered from panic rendering window", 
						slog.Any("panic", r),
						slog.Int("windowID", windowID))
				}
			}()

			windowItem := item.windowToSketchybar(isFocusedWorkspace, monitorID, workspace.Workspace, window.App)
			sketchybarWindowID := getSketchybarWindowID(windowID)

			isNewWindow := !item.renderedItems[sketchybarWindowID]
			if isNewWindow {
				initialWindowItem := *windowItem
				initialWindowItem.Width = pointer(0)
				initialWindowItem.Icon.Drawing = "off"

				*batches = batch(*batches, s("--add", "item", sketchybarWindowID, position))
				*batches = batch(*batches, m(s("--set", sketchybarWindowID), initialWindowItem.ToArgs()))
			}

			*batches = batch(*batches, s("--move", sketchybarWindowID, "after", prevSketchybarItemID))
			*batches = batch(*batches, m(
				s("--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime, "--set", sketchybarWindowID),
				windowItem.ToArgs(),
			))

			prevSketchybarItemID = sketchybarWindowID
		}()
	}
}

func (item *AerospaceItem) handleBracketsAndSpacersSafely(
	ctx context.Context,
	batches *Batches,
	workspace *aerospace.WorkspaceWithWindowIDs,
	isFocusedWorkspace bool,
	position sketchybar.Position,
) {
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(ctx, "aerospace item: recovered from panic in handleBracketsAndSpacersSafely", slog.Any("panic", r))
		}
	}()

	bracketSpacerID := getSketchybarBracketSpacerID(workspace.Workspace)
	if !item.renderedItems[bracketSpacerID] {
		*batches = item.addBracketSpacer(*batches, workspace.Workspace, position)
	}

	sketchybarBracketID := getSketchybarBracketID(workspace.Workspace)
	if !item.renderedItems[sketchybarBracketID] {
		*batches = item.addWorkspaceBracket(*batches, isFocusedWorkspace, workspace.Workspace)
	}

	// Handle bracket state with error recovery
	func() {
		defer func() {
			if r := recover(); r != nil {
				item.logger.ErrorContext(ctx, "aerospace item: recovered from panic handling bracket state", slog.Any("panic", r))
			}
		}()

		sketchybarWindowIDs := make([]string, len(workspace.Windows))
		for i, windowID := range workspace.Windows {
			sketchybarWindowIDs[i] = getSketchybarWindowID(windowID)
		}

		*batches = item.handleWorkspaceBracket(*batches, workspace, sketchybarWindowIDs, isFocusedWorkspace, time.Millisecond*5, time.Now())
	}()

	if len(workspace.Windows) == 0 {
		item.cleanupWorkspaceBracket(workspace.Workspace)
	}
}

func (item *AerospaceItem) createFallbackBatches(batches Batches, position sketchybar.Position) Batches {
	// Create minimal fallback UI when everything fails
	item.logger.InfoContext(context.Background(), "aerospace item: creating fallback batches")
	
	defer func() {
		if r := recover(); r != nil {
			item.logger.ErrorContext(context.Background(), "aerospace item: recovered from panic in createFallbackBatches", slog.Any("panic", r))
		}
	}()

	// Just add a basic spacer to prevent complete failure
	spacerItem := sketchybar.ItemOptions{
		Width:      pointer(*settings.Sketchybar.ItemSpacing),
		Background: sketchybar.BackgroundOptions{Drawing: "off"},
	}
	
	fallbackID := "aerospace.fallback"
	batches = batch(batches, s("--add", "item", fallbackID, position))
	batches = batch(batches, m(s("--set", fallbackID), spacerItem.ToArgs()))
	
	return batches
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
	colors := item.getWorkspaceColors(isFocusedWorkspace)

	borderColor := colors.backgroundColor
	if len(workspace.Windows) == 0 {
		borderColor = colorsPkg.Transparent
	}

	// Always animate the color to handle visibility and focus changes
	batches = batch(batches, s(
		"--animate", sketchybar.AnimationTanh, settings.Sketchybar.Aerospace.TransitionTime,
		"--set", sketchybarBracketID,
		fmt.Sprintf("background.border_color=%s", borderColor),
	))

	// Update the internal state for the next render cycle.
	item.workspaceWindowIDs[workspace.Workspace] = sketchybarWindowIDs

	return batches
}

// Clean up workspace bracket state
func (item *AerospaceItem) cleanupWorkspaceBracket(workspaceID string) {
	delete(item.workspaceWindowIDs, workspaceID)
	delete(item.bracketStates, workspaceID)
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

func getSketchybarBracketSpacerID(spaceID aerospace.WorkspaceID) string {
	return fmt.Sprintf("%s.%s", bracketSpacerItemPrefix, spaceID)
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
	bracketSpacerID := getSketchybarBracketSpacerID(workspaceID)

	// The bracket is defined by the workspace icon and the spacer.
	// Windows will be moved between these two items.
	itemsForBracket := []string{sketchybarSpaceID, bracketSpacerID}

	item.logger.Debug("Adding workspace bracket",
		slog.String("workspace", workspaceID),
		slog.String("bracketID", sketchybarBracketID),
		slog.Any("items", itemsForBracket))

	batches = batch(batches, m(s(
		"--add",
		"bracket",
		sketchybarBracketID),
		itemsForBracket,
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

func (item *AerospaceItem) addBracketSpacer(
	batches Batches,
	workspaceID string,
	position sketchybar.Position,
) Batches {
	bracketSpacerItem := sketchybar.ItemOptions{
		Width: pointer(0), // Initially zero width
		Background: sketchybar.BackgroundOptions{
			Drawing: "off",
		},
	}

	sketchybarSpacerID := getSketchybarBracketSpacerID(workspaceID)
	batches = batch(batches, s(
		"--add",
		"item",
		sketchybarSpacerID,
		position,
	))
	batches = batch(batches, m(s(
		"--set",
		sketchybarSpacerID,
	), bracketSpacerItem.ToArgs()))

	return batches
}

func isAerospace(name string) bool {
	return name == AerospaceName
}

// Helper function to create pointer to int
func pointer[T any](v T) *T {
	return &v
}



var _ WentsketchyItem = (*AerospaceItem)(nil)