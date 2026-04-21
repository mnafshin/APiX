# APiX Keyboard Shortcuts

This document describes the keyboard shortcuts available in the APiX VS Code extension.

## Traffic Management

| Action | Windows/Linux | macOS |
|--------|---------------|-------|
| Open Traffic Inspector | `Ctrl+Shift+X` | `Cmd+Shift+X` |
| Clear Traffic History | `Ctrl+Shift+L` | `Cmd+Shift+L` |
| Refresh Traffic View | `Ctrl+Shift+R` | `Cmd+Shift+R` |
| Export Traffic as HAR | `Ctrl+Shift+S` | `Cmd+Shift+S` |

## Request Operations

| Action | Windows/Linux | macOS |
|--------|---------------|-------|
| Open Request Composer | `Ctrl+Shift+N` | `Cmd+Shift+N` |
| Copy Request as curl | `Ctrl+Shift+C` | `Cmd+Shift+C` |
| Replay Request | `Ctrl+Shift+E` | `Cmd+Shift+E` |

## Breakpoints

| Action | Windows/Linux | macOS |
|--------|---------------|-------|
| Add URL Breakpoint | `Ctrl+Shift+B` | `Cmd+Shift+B` |

## Context

Most shortcuts are context-aware and only work when focused on the relevant view:

- **Traffic shortcuts** (`Ctrl+Shift+L`, `Ctrl+Shift+R`, `Ctrl+Shift+S`): Work in the Traffic view
- **Copy Request as curl** (`Ctrl+Shift+C`): Active when a traffic item is selected
- **Replay Request** (`Ctrl+Shift+E`): Active when a traffic item is selected
- **Add URL Breakpoint** (`Ctrl+Shift+B`): Active when focused on the Breakpoints view
- **Open Traffic Inspector** (`Ctrl+Shift+X`): Works globally (except when traffic panel is already focused)

## Tips

- Use **`Ctrl+Shift+N`** / **`Cmd+Shift+N`** to open the Request Composer for manual request creation
- **Replay** a request with **`Ctrl+Shift+E`** / **`Cmd+Shift+E`** to resend it with optional modifications
- **Copy as curl** with **`Ctrl+Shift+C`** / **`Cmd+Shift+C`** to export requests as curl commands for use in terminal or scripts

## Customization

To customize these shortcuts, you can use the **Keyboard Shortcuts** editor in VS Code:

1. Press `Ctrl+K Ctrl+S` (Windows/Linux) or `Cmd+K Cmd+S` (macOS) to open the Keyboard Shortcuts editor
2. Search for "apix" to find all APiX-related shortcuts
3. Double-click on any shortcut to edit or remove it
4. Type a new key combination to rebind it

All APiX commands are prefixed with `apix.` in the command palette.
