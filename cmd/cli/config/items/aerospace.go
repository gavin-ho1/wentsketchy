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
	logger     *slog.Logger
	aerospace  aerospace.Aerospace
	sketchybar sketchybar.API

	position  sketchybar.Position
		windowIDs map[aerospace.WindowID]aerospace.WorkspaceID
	syncCallback func(context.Context, *args.In)

	// Add these new fields for polling
	pollTimer    *time.Timer
	pollMutex    sync.Mutex
	lastPollTime time.Time
	isPolling    bool
}

const (
	pollDelay = 100 * time.Millisecond  // Wait 500ms before polling
	pollDebounce = 10 * time.Millisecond // Minimum time between polls
)

func (item *AerospaceItem) startDelayedPoll(ctx context.Context) {
	item.pollMutex.Lock()
	defer item.pollMutex.Unlock()
	
	// Cancel existing timer if running
	if item.pollTimer != nil {
		item.pollTimer.Stop()
	}
	
	// Don't start new poll if we just polled recently
	if time.Since(item.lastPollTime) < pollDebounce {
		return
	}
	
	item.pollTimer = time.AfterFunc(pollDelay, func() {
		item.pollMutex.Lock()
		defer item.pollMutex.Unlock()
		
		if item.isPolling {
			return // Already polling
		}
		
		item.isPolling = true
		item.lastPollTime = time.Now()
		
		// Perform the polling check
		go func() {
			defer func() {
				item.pollMutex.Lock()
				item.isPolling = false
				item.pollMutex.Unlock()
			}()
			
			item.logger.Debug("Starting delayed poll for aerospace sync")
			
			// Create a synthetic event to trigger tree check
			syntheticArgs := &args.In{
				Name:  AerospaceName,
				Event: events.SpaceWindowsChange,
				Info:  "",
			}
			
			// This will trigger the normal Update flow
			if callback := item.syncCallback; callback != nil {
				callback(ctx, syntheticArgs)
			}
		}()
	})
}



func (item *AerospaceItem) performSyncCheck(ctx context.Context, batches Batches) (Batches, error) {
	item.logger.Debug("Performing sync check")
	
	// Force refresh the tree
	item.aerospace.SingleFlightRefreshTree()
	
	// Run the normal CheckTree logic
	return item.CheckTree(ctx, batches)
}

func NewAerospaceItem(
	logger *slog.Logger,
	aerospace aerospace.Aerospace,
	sketchybarAPI sketchybar.API,
) AerospaceItem {
	return AerospaceItem{
		logger,
		aerospace,
		sketchybarAPI,
		sketchybar.PositionLeft,
		make(map[int]string, 0),
		nil, // syncCallback
		nil,  // pollTimer
		sync.Mutex{}, // pollMutex
		time.Time{}, // lastPollTime
		false, // isPolling
	}
}

const aerospaceCheckerItemName = "aerospace.checker"
const workspaceItemPrefix = "aerospace.workspace"
const windowItemPrefix = "aerospace.window"
const bracketItemPrefix = "aerospace.bracket"
const spacerItemPrefix = "aerospace.spacer"

const AerospaceName = aerospaceCheckerItemName

func (item AerospaceItem) Init(
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

func (item AerospaceItem) Update(
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
	if args.Event == aerospace_events.WorkspaceChange {
		var data aerospace_events.WorkspaceChangeEventInfo
		err := json.Unmarshal([]byte(args.Info), &data)

		if err != nil {
			return batches, fmt.Errorf("aerospace: could not deserialize json for workspace-change. %v", args.Info)
		}

		// Set focused workspace immediately to ensure state consistency
		item.aerospace.SetFocusedWorkspaceID(data.Focused)
		
		batches = item.handleWorkspaceChange(ctx, batches, data.Prev, data.Focused)
	}

	if args.Event == events.SpaceWindowsChange {
		batches, err = item.CheckTree(ctx, batches)

		if err != nil {
			return batches, err
		}
	}

	if args.Event == events.DisplayChange {
		batches = item.handleDisplayChange(batches)
	}

	if args.Event == events.FrontAppSwitched {
		batches = item.handleFrontAppSwitched(ctx, batches, args.Info)
	}

	return batches, err
}

// updateWorkspaceBracket ensures the bracket for a given workspace is correctly
// displayed. It creates, updates, or removes the bracket based on whether the
// workspace contains windows and whether it is focused.
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
        // Remove bracket if workspace has no windows
        batches = batch(batches, s("--remove", sketchybarBracketID))
        return batches
    }

    // Always remove and recreate bracket to ensure consistency
    batches = batch(batches, s("--remove", sketchybarBracketID))
    
    // Create members list
    members := []string{getSketchybarWorkspaceID(workspaceID)}
    for _, windowID := range workspace.Windows {
        members = append(members, getSketchybarWindowID(windowID))
    }

    // Add bracket with all members
    batches = batch(batches, m(s(
        "--add", "bracket", sketchybarBracketID, getSketchybarWorkspaceID(workspaceID)),
        members[1:], // Window IDs
    ))

    // Set bracket visual properties
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

func (item AerospaceItem) CheckTree(
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
	// Track workspaces that need bracket updates
	workspacesNeedingUpdate := make(map[aerospace.WorkspaceID]bool)
	
	// First pass: Handle moved windows
	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue // skip workspaces not defined in the icons.Workspace map
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

						// Mark both workspaces as needing bracket updates
						workspacesNeedingUpdate[currentWorkspaceID] = true
						workspacesNeedingUpdate[workspace.Workspace] = true

						// Update window properties for new workspace state
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

						// Move window to new position
						batches = batch(batches, s(
							"--move",
							getSketchybarWindowID(windowID),
							"after",
							getSketchybarWorkspaceID(workspace.Workspace),
						))

						// Update display property for new monitor
						batches = batch(batches, s(
							"--set",
							getSketchybarWindowID(windowID),
							"display="+strconv.Itoa(monitor.Monitor),
						))

						// Update internal state
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

	// Second pass: Remove deleted windows
	for windowID := range item.windowIDs {
		if _, found := tree.IndexedWindows[windowID]; !found {
			item.logger.Info("Removing window", slog.Int("windowID", windowID))
			oldWorkspaceID := item.windowIDs[windowID]
			batches = item.removeWindow(batches, windowID)
			workspacesNeedingUpdate[oldWorkspaceID] = true
		}
	}

	// Third pass: Update visibility for all windows
	batches = item.updateWindowVisibility(ctx, batches, focusedWorkspaceID)

	// Update brackets for affected workspaces
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
) Batches {  // Return Batches instead of void
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

func (item AerospaceItem) handleFrontAppSwitched(
    ctx context.Context,
    batches Batches,
    windowApp string,
) Batches {
    item.aerospace.SetFocusedApp(windowApp)
    
    // CRITICAL: Refresh tree to ensure consistency
    item.aerospace.SingleFlightRefreshTree()
    
    focusedWorkspaceID := item.aerospace.GetFocusedWorkspaceID(ctx)
    batches = item.highlightWindows(batches, focusedWorkspaceID, windowApp)
    
    // ADD POLLING: Start delayed poll to catch any missed changes
    item.startDelayedPoll(ctx)
    
    return batches
}


func (item AerospaceItem) workspaceToSketchybar(
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

func (item AerospaceItem) getWorkspaceColors(isFocusedWorkspace bool) workspaceColors {
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

func (item AerospaceItem) getWindowVisibility(isFocusedWorkspace bool) *windowVisibility {
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

func (item AerospaceItem) handleWorkspaceChange(
	ctx context.Context,
	batches Batches,
	prevWorkspaceID string,
	focusedWorkspaceID string,
) Batches {
	// CRITICAL: Refresh the tree first to get updated window positions
	item.aerospace.SingleFlightRefreshTree()
	tree := item.aerospace.GetTree()

	if tree == nil {
		item.logger.Error("Tree is nil during workspace change")
		return batches
	}

	// Track workspaces that need bracket updates due to window movements
	workspacesNeedingUpdate := make(map[aerospace.WorkspaceID]bool)

	// First, handle any window movements that occurred during the workspace change
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

					// Mark both workspaces as needing bracket updates
					workspacesNeedingUpdate[currentWorkspaceID] = true
					workspacesNeedingUpdate[workspace.Workspace] = true

					// Move window to new position (no animation needed for positioning)
					batches = batch(batches, s(
						"--move",
						getSketchybarWindowID(windowID),
						"after",
						getSketchybarWorkspaceID(workspace.Workspace),
					))

					// Update display property for new monitor
					batches = batch(batches, s(
						"--set",
						getSketchybarWindowID(windowID),
						"display="+strconv.Itoa(monitor.Monitor),
					))

					// Update internal state
					item.windowIDs[windowID] = workspace.Workspace
				}
			}
		}
	}

	// Update/create brackets for workspaces with moved windows
	for workspaceID := range workspacesNeedingUpdate {
		isFocused := workspaceID == focusedWorkspaceID
		batches = item.updateWorkspaceBracket(batches, workspaceID, isFocused)
	}

	// Now update workspace visual states and window visibility with animations
	for _, monitor := range tree.Monitors {
		for _, workspace := range monitor.Workspaces {
			if _, ok := icons.Workspace[workspace.Workspace]; !ok {
				continue
			}
			
			isFocusedWorkspace := workspace.Workspace == focusedWorkspaceID

			// Update workspace visual state with animation
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

			// Update bracket visual state with animation
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

			// Update window visibility with animation
			windowVisibility := item.getWindowVisibility(isFocusedWorkspace)
			for _, windowID := range workspace.Windows {
				window := tree.IndexedWindows[windowID]
				if window == nil {
					continue
				}

				sketchybarWindowID := getSketchybarWindowID(windowID)
				
				// Update window properties for the new workspace context
				windowItem := item.windowToSketchybar(
					isFocusedWorkspace,
					monitor.Monitor,
					workspace.Workspace,
					window.App,
				)

				// Apply the visibility settings
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
	
	// ADD POLLING: Start delayed poll to catch any missed changes
	item.startDelayedPoll(ctx)
	
	return batches
}

func (item AerospaceItem) getSketchybarDisplayIndex(
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

func (item AerospaceItem) handleDisplayChange(batches Batches) Batches {
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

	// Ensure all brackets are consistent after display change
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