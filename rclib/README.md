# grclib

`grclib` is a C ABI DLL for building Remote Control clients for GServer-style tooling. It owns the protocol details: packet framing, string encodings, file-browser compression, account/player editors, PMs, NC scripts, classes, weapons, NPCs, IRC, bans, and server data.

Python, C#, Rust, or any other client should call typed functions and callbacks instead of building or parsing raw protocol packets itself.

The game-server login PCID uses stable machine-id sources and reversed-per-byte MD5 rendering expected by compatible servers. Windows uses the native RC sources; Linux/macOS use local machine, network, and root-device identifiers.

License: LGPL-2.0-only. See `LICENSE` and `THIRD_PARTY_NOTICES.md`.

## Build

Windows with CMake and Visual Studio:

```bat
cmake -S . -B build -G "Visual Studio 18 2026" -A x64
cmake --build build --config Release
```

Linux/macOS with CMake:

```sh
cmake -S . -B build
cmake --build build --config Release
```

The build outputs the shared library to `bin/` in the repository root (`grclib.dll`, `grclib.so`, or `grclib.dylib`, depending on platform). Copy it next to the wrapper or application that loads it.

## Public Contract

- `rc_on_message` is only for real `PLO_RC_CHAT` text.
- `rc_on_private_message` receives private messages with sender id, account, nick, and text.
- `rc_on_server_data` carries non-chat server data such as `toall`, `admin_message`, `nc_message`, `options`, `flags`, `folder_config`, and miscellaneous protocol data.
- `rc_on_filebrowser_*` callbacks own file-browser folders, files, messages, downloads, and upload status.
- `rc_on_script_received` returns NPC, class, and weapon scripts.
- `rc_on_player_rights`, `rc_on_player_text_data`, and `rc_on_player_attributes` own player editors.
- `rc_on_account_list` returns newline-separated account names from Accounts List queries.

Debug escape hatches (`rc_send_raw_packet`, `rc_send_nc_packet`, `rc_on_raw_packet`) exist for tooling and diagnostics. Normal clients should use typed APIs.

## Core Flow

1. `rc_connect(listserver_host, listserver_port, account, password)`
2. `rc_get_servers(...)`
3. `rc_connect_to_server(handle, server_index)`
4. Register callbacks.
5. Pump `rc_process_events(handle)` regularly from the UI/event loop.
6. Call `rc_disconnect(handle)` when done.

If your target server expects the newer login/ban-list behavior, leave the default protocol mode enabled. For older targets, call `rc_set_new_protocol(handle, 0)` after `rc_connect(...)` and before the server connection.

## API Reference

All strings are `char*` / `const char*` encoded as protocol-compatible single-byte text. Any `char*` returned by grclib must be released with `rc_free`.

### Connection

| API | Purpose |
| --- | --- |
| `rc_connect` | Connects to a list endpoint and stores the returned server list. |
| `rc_get_servers` | Returns the available server list for a connected handle. |
| `rc_connect_to_server` | Connects/authenticates to one server from the list. |
| `rc_connect_to_nc_server` | Opens the NC/script socket for the active server. |
| `rc_disconnect_nc` | Closes the NC/script socket. |
| `rc_disconnect` | Disconnects all sockets and frees the handle. |
| `rc_process_events` | Runs queued callbacks; call this regularly from your UI loop. |
| `rc_is_connected` | Returns whether the game socket is connected. |
| `rc_is_authenticated` | Returns whether the game socket finished login. |
| `rc_is_nc_connected` | Returns whether the NC socket is connected/authenticated. |
| `rc_last_error` | Returns the last connection/API error string for the handle. |
| `rc_is_new_protocol` | Reads the current newer-protocol compatibility flag. |
| `rc_set_new_protocol` | Enables/disables newer-protocol compatibility before server login. |

### Callback Registration

| API | Purpose |
| --- | --- |
| `rc_on_connected` | Fires when server login completes. |
| `rc_on_disconnected` | Fires when the server disconnects or the client closes. |
| `rc_on_player_joined` | Fires when a player appears in the player list. |
| `rc_on_player_left` | Fires when a player leaves the player list. |
| `rc_on_message` | Fires only for real RC chat lines. |
| `rc_on_private_message` | Fires for incoming PMs with sender id/account/nick/text. |
| `rc_on_file_received` | Fires when a file download completes. |
| `rc_on_weapon_added` | Fires when a weapon is added to the NC cache. |
| `rc_on_weapon_deleted` | Fires when a weapon is removed. |
| `rc_on_class_added` | Fires when a class is added to the NC cache. |
| `rc_on_class_deleted` | Fires when a class is removed. |
| `rc_on_npc_added` | Fires when an NPC is added to the NC cache. |
| `rc_on_npc_deleted` | Fires when an NPC is removed. |
| `rc_on_npc_attributes` | Fires with attributes for a requested NPC. |
| `rc_on_player_prop_changed` | Fires when a player property update arrives. |
| `rc_on_world_time` | Fires for world-time updates. |
| `rc_on_max_upload_file_size` | Fires when the server reports the max upload size. |
| `rc_on_command_response` | Fires for command/response text not represented by a richer callback. |
| `rc_on_raw_packet` | Optional debug callback for raw packet display. |
| `rc_on_pm_servers_updated` | Fires when the PM-server list changes. |
| `rc_on_npc_flags` | Fires with flags for a requested NPC. |
| `rc_on_pm_server_players` | Fires with the player list for a PM server. |
| `rc_on_filebrowser_folders` | Fires when file-browser folder patterns are available. |
| `rc_on_filebrowser_files` | Fires when a folder listing is available. |
| `rc_on_filebrowser_message` | Fires for file-browser status messages. |
| `rc_on_script_received` | Fires for requested NPC/class/weapon script text. |
| `rc_on_server_data` | Fires for typed non-chat data such as toalls, admin messages, NC text, options, flags, and fallback protocol data. |
| `rc_on_player_rights` | Fires with parsed player rights data. |
| `rc_on_player_text_data` | Fires with player comments, account text, profile text, and similar text blocks. |
| `rc_on_player_attributes` | Fires with player attributes as JSON plus editor text. |
| `rc_on_local_npcs` | Fires with local NPC/level dump data. |
| `rc_on_irc_message` | Fires with IRC channel lines. |
| `rc_on_ban_data` | Fires with ban details for a requested player/account. |
| `rc_on_ban_list_data` | Fires with ban-list/history/activity data. |
| `rc_on_account_list` | Fires with newline-separated account names from an account search. |

### Cached Lists And State

| API | Purpose |
| --- | --- |
| `rc_get_players` | Copies the current player cache. |
| `rc_get_weapons` | Copies the current weapon cache. |
| `rc_get_classes` | Copies the current class cache. |
| `rc_get_npcs` | Copies the current NPC cache. |
| `rc_get_levels` | Copies the current level cache. |
| `rc_get_pm_servers` | Copies the current PM-server list. |
| `rc_get_cached_npc_flags` | Returns cached flags for an NPC id. |
| `rc_get_filebrowser_folders` | Returns the current file-browser folder pattern cache. |
| `rc_get_filebrowser_files` | Returns the current file-browser file cache. |
| `rc_copy_filebrowser_folders` | Copies file-browser folder entries for caller ownership. |
| `rc_copy_filebrowser_files` | Copies file-browser file entries for caller ownership. |
| `rc_free_filebrowser_folders` | Frees folder entries returned by file-browser copy APIs. |
| `rc_free_filebrowser_files` | Frees file entries returned by file-browser copy APIs. |
| `rc_get_server_options` | Returns cached server options text. |
| `rc_get_server_flags` | Returns cached server flags text. |
| `rc_get_folder_config` | Returns cached folder config text. |
| `rc_get_max_upload_file_size` | Returns the latest max upload size value. |

### Chat, Players, And Admin Actions

| API | Purpose |
| --- | --- |
| `rc_execute` | Sends a normal RC command/chat line. |
| `rc_send_private_message` | Sends a PM to one player id. |
| `rc_send_mass_pm` | Sends one bulk PM packet to many player ids. |
| `rc_send_toall_message` | Sends a toall/global server message. |
| `rc_send_admin_message` | Sends an admin message to one player id. |
| `rc_send_admin_message_all` | Sends an admin message to all players. |
| `rc_set_nickname` | Changes the current RC nickname. |
| `rc_warp_player` | Warps a player to a level/x/y location. |
| `rc_disconnect_player` | Disconnects a player with a reason. |
| `rc_reset_player` | Resets a player by account. |

### Player Editors And Accounts

| API | Purpose |
| --- | --- |
| `rc_request_player_rights` | Requests editable rights for an account. |
| `rc_set_player_rights` | Saves rights, IP range, and folder access for an account. |
| `rc_request_player_attrs` | Requests player attributes. |
| `rc_set_player_attributes` | Saves player attributes from JSON. |
| `rc_request_player_comments` | Requests player comments. |
| `rc_set_player_comments` | Saves player comments. |
| `rc_request_player_account` | Requests account editor data. |
| `rc_set_player_account` | Saves account editor data for an existing account. |
| `rc_add_player_account` | Creates a new account from account editor text. |
| `rc_request_player_profile` | Requests player profile text. |
| `rc_set_player_profile` | Saves player profile text. |
| `rc_request_account_list` | Searches account names by account filter and conditions. |
| `rc_format_player_rights_text` | Builds user-editable rights text from parsed rights data. |
| `rc_parse_player_rights_text` | Parses editable rights text back into serialized data. |
| `rc_format_player_account_text` | Builds editable account text from account data. |
| `rc_parse_player_account_text` | Parses editable account text back into serialized data. |
| `rc_format_player_attributes_text` | Builds editable attributes text from attributes JSON. |
| `rc_parse_player_attributes_text` | Parses editable attributes text back into attributes JSON. |

### File Browser And Levels

| API | Purpose |
| --- | --- |
| `rc_filebrowser_start` | Starts/refreshes the file browser. |
| `rc_filebrowser_cd` | Requests a file-browser folder listing. |
| `rc_filebrowser_download` | Downloads a file-browser path. |
| `rc_filebrowser_delete` | Deletes a file-browser path. |
| `rc_filebrowser_rename` | Renames/moves a file-browser path. |
| `rc_upload_file` | Uploads raw file content to a server path. |
| `rc_download_file` | Downloads a raw server path. |
| `rc_upload_level` | Convenience wrapper for uploading a level file. |
| `rc_download_level` | Convenience wrapper for downloading a level file. |

### NC, Scripts, Classes, Weapons, And NPCs

| API | Purpose |
| --- | --- |
| `rc_request_server_list` | Requests a refreshed server list from the connected server. |
| `rc_request_pm_server_list` | Requests PM-server names. |
| `rc_request_pm_server_players` | Requests players connected through a PM server. |
| `rc_add_weapon` | Creates/uploads a weapon script and image name. |
| `rc_delete_weapon` | Deletes a weapon. |
| `rc_update_weapon` | Updates a weapon script and image name. |
| `rc_request_weapon_script` | Requests a weapon script. |
| `rc_add_class` | Creates/uploads a class script. |
| `rc_delete_class` | Deletes a class. |
| `rc_update_class` | Updates a class script. |
| `rc_request_class_script` | Requests a class script. |
| `rc_create_npc_on_server` | Creates an NPC on the NPC server. |
| `rc_delete_npc` | Deletes an NPC by id. |
| `rc_update_npc` | Updates an NPC script by id. |
| `rc_request_npc_script` | Requests an NPC script by id. |
| `rc_request_npc_attributes` | Requests NPC attributes by id. |
| `rc_reset_npc` | Resets/restarts an NPC by id. |
| `rc_warp_npc` | Warps an NPC to a level/x/y location. |
| `rc_get_npc_flags` | Requests NPC flags by id. |
| `rc_set_npc_flags` | Saves NPC flags by id. |
| `rc_request_local_npcs` | Requests local NPC/level dump data for a level. |

### Server Config, IRC, And Bans

| API | Purpose |
| --- | --- |
| `rc_request_server_options` | Requests server options text. |
| `rc_upload_server_options` | Saves server options text. |
| `rc_request_server_flags` | Requests server flags text. |
| `rc_upload_server_flags` | Saves server flags text. |
| `rc_request_folder_config` | Requests folder configuration text. |
| `rc_upload_folder_config` | Saves folder configuration text. |
| `rc_send_irc_text` | Sends a raw IRC command through the typed IRC text path. |
| `rc_irc_login` | Logs into IRC. |
| `rc_irc_join` | Joins an IRC channel. |
| `rc_irc_part` | Parts an IRC channel. |
| `rc_request_player_ban` | Requests ban data by account/player id. |
| `rc_request_player_ban_by_account` | Requests ban data by account. |
| `rc_request_ban_types` | Requests available ban types. |
| `rc_request_ban_history` | Requests ban history for an account. |
| `rc_request_staff_activity` | Requests staff activity for an account. |
| `rc_set_ban` | Saves newer-protocol ban data. |
| `rc_set_legacy_player_ban` | Saves legacy player ban data. |

### Debug And Encoding Helpers

| API | Purpose |
| --- | --- |
| `rc_send_raw_packet` | Debug escape hatch for sending a raw game-server packet. |
| `rc_send_nc_packet` | Debug escape hatch for sending a raw NC packet. |
| `rc_gtokenize` | Encodes newline text into protocol comma-text. |
| `rc_gtokenize_reverse` | Decodes protocol comma-text into newline text. |
| `rc_get_rights_names` | Returns known rights names for UI display. |
| `rc_get_color_names` | Returns known color names for UI display. |
| `rc_get_packet_names` | Returns packet id/name pairs for debug displays. |
| `rc_get_1plus_text_net_string` | Encodes a one-plus length-prefixed string. |
| `rc_read_gbyte` | Reads a protocol GByte from raw data. |
| `rc_read_gshort` | Reads a protocol GShort from raw data. |
| `rc_read_gint5` | Reads a protocol GInt5 from raw data. |
| `rc_read_length_string` | Reads a length-prefixed string from raw data. |
| `rc_read_comma_text` | Reads and decodes comma-text from raw data. |
| `rc_free` | Frees memory returned by grclib. |

See `examples\python_example.py` for a compact ctypes client.
