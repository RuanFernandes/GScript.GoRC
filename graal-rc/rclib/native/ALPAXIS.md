# Alpaxis grclib (windows-amd64 only)

`windows-amd64/grclib64.dll` in this branch is **not** the library of the
release named in `manifest.json`. It was built locally from GRClib `v1.0.51`
plus one fix that is not committed to any repository: a packet or raw block that
continues in the next frame is kept instead of dropped. Without it, large File
Browser folders never open on Alpaxis. A rebuild needs those local source
changes; a plain `v1.0.51` does not have the fix.

Build, from the GScript.RClib sources with the fix applied:

    cmake -S . -B build-static -G "Visual Studio 18 2026" -A x64 -DCMAKE_POLICY_DEFAULT_CMP0091=NEW -DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded
    cmake --build build-static --config Release --target grclib

Copy `bin/grclib.dll` over `windows-amd64/grclib64.dll` and update its sha256
in `manifest.json`. Do not run `fetch-grclib -force` or with another
`-release`: it would put the official library back. The other platforms still
ship the official libraries, without this fix.
