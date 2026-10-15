# Key reference

`cftui` has three views: the **tunnel list**, the **route editor** for a
selected tunnel, and the **add-route form**. A key does different things
depending on which view is open.

Press `?` at any time for the same reference in-app.

## Modal help

The help panel is **modal**. While it is open, `cftui` ignores every other key —
pressing `n`, `d`, or `w` behind the panel does nothing rather than silently
acting on the list underneath.

| Key | Action |
|---|---|
| `?` | open help; close it if open |
| `esc` | close help (if open), otherwise go back |
| `q` | close help (if open), otherwise quit |
| `ctrl+c` | quit immediately, even with help open |

## Tunnel list

| Key | Action |
|---|---|
| `n` | create a tunnel (name is validated) |
| `d` | delete the selected tunnel (type its name to confirm) |
| `enter` | open the selected tunnel's routes |
| `t` | show the connector token, to run this tunnel on another machine |
| `r` | refresh the list |
| `↑` / `↓` (or `k` / `j`) | move the selection |
| `?` | open help |

## Route editor

| Key | Action |
|---|---|
| `a` | add a route; the local `PORT` field is focused first |
| `o` | open the selected route's public URL in a browser |
| `J` / `K` | move the selected route down / up (the catch-all stays last) |
| `p` | pin the selected route to the top |
| `w` | apply staged changes |
| `u` | discard staged changes |
| `/` | filter routes by host, path, or service |
| `esc` | back to the tunnel list |
| `?` | open help |

## Add-route form

| Key | Action |
|---|---|
| `tab` | next field (port, hostname, service) |
| `shift+tab` | previous field |
| `enter` | stage the route |
| `esc` | cancel |
| `?` | open help |

## Connector token panel

Opened with `t` on the tunnel list.

| Key | Action |
|---|---|
| `v` | reveal / hide the token |
| `t` | close the panel |
| `esc` | close the panel |
| `q` | close the panel |
| `ctrl+c` | quit |

The token is masked by default (`eyJh••••••••••••1ZmI`) so it cannot be captured
by a shoulder-surfer or a screen recording. `cftui` never writes it to disk or
to a log.

## Delete confirmation

Deleting a tunnel is irreversible. `cftui` asks you to type the tunnel's exact
name first, so a stray `y` cannot destroy anything.

| Key | Action |
|---|---|
| `<name>` | type the tunnel name, then `enter` to confirm |
| `backspace` | delete a character |
| `esc` | cancel |
