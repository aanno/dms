# Snapcast sessions in dms (proof of concept)

This fork of dms can offer **synchronized multiroom playback**: renderers that play
the same item join one [Snapcast](https://github.com/badaix/snapcast) session and
play in sync. Pause, seek and the end of the track act on every room in the session.

It is a proof of concept of
[gstreamer-snapcast-rs](https://github.com/aanno/gstreamer-snapcast-rs), which also
has the design (`docs/dlna.md`), the protocol (`docs/dlna-snapcast-protocol.md`) and
the tests (`docs/dlna-tests.md`). The matching renderer is the
[gmrender-resurrect fork](https://github.com/aanno/gmrender-resurrect) with
`--snapcast-sessions`. Control points (e.g. BubbleUPnP) need no changes.

## How it works

1. The listing is unchanged: each item has its ordinary `/res?path=…` resource.
2. A renderer that can play Snapcast sessions asks for that URL with
   `Accept: audio/x-snapcast-tcp`, first with `HEAD`, then with `GET`.
3. dms answers that `GET` with the session's URI instead of the media, e.g.
   `snapcast-tcp://192.168.1.2:1704/dms-2b5de2b3edd9?control=1705`.
4. Before answering, dms opens the session: it starts a **helper process** that plays
   the file into that Snapcast stream. Each item has its own stream, named
   `dms-<hash of its path>`. A second request for the same item reuses the running
   session; that's how a second room joins.
5. The helper obeys the session's play, pause and seek itself (they reach it
   through the Snapcast server), and it exits when the session has been idle for a
   while.
6. Any other request gets the media as before. If the helper can't start, the
   renderer gets the media too.

Code: `dlna/dms/snapcast.go` (the negotiation) and `dlna/dms/snapcast_feeder.go` (the
`SessionFeeder` interface and `ProcessFeeder`, the helper process).

## Options

| Option | Meaning |
|---|---|
| `-snapcastStream <uri>` | The session URI offered to renderers. `{host}` is this machine (its IPv4 address if it has one), and `{stream}` is the item's stream. Repeatable, once per Snapcast transport (`snapcast-tcp://`, later `snapcast-ws://`), most preferred first |
| `-snapcastFeeder "<command> <options>"` | The helper and its options, **quoted as one argument**. dms appends `--paused <file> <stream>`. Needs `{stream}` in `-snapcastStream` |

Without `{stream}`, every item points at one fixed stream that something else feeds,
and `-snapcastFeeder` isn't used. That's useful for testing renderers.

Example:

```sh
dms -path ~/Music -friendlyName "dms snapcast" \
    -snapcastStream 'snapcast-tcp://{host}:1704/{stream}?control=1705' \
    -snapcastFeeder "$HOME/.local/bin/session-sender --mode shared --idle-exit 600"
```

## The helper: `session-sender`

- **Code:** `crates/gst-plugin-snapsink/examples/session-sender.rs` in
  gstreamer-snapcast-rs (a cargo example of the `snapsink` crate). It plays a file
  with GStreamer into `snapsink`.
- **Building it,** from the root of gstreamer-snapcast-rs:

  ```sh
  make dlna-build    # also builds dms and gmrender; links target/dlna/session-sender
  # or only the helper:
  cargo build --release -p gst-plugin-snapsink --features control --example session-sender
  # -> target/release/examples/session-sender
  ```

- **Where it must be:** anywhere. dms runs the first word of `-snapcastFeeder`:
  - **a path:** absolute, or relative to the directory dms was started in (not
    `-path`);
  - **or a name on `PATH`:** e.g. after
    `cp target/release/examples/session-sender ~/.local/bin/`, just `session-sender`.

  If dms can't find it, it warns at startup, and renderers get the media instead of
  sessions.
- **Options dms should pass:**
  - **`--mode shared`:** all sessions share the per-user Snapcast server
    (`gst-snapcast-daemon`) on one port. **Use it.** Without it, the helper runs a
    server of its own on 1704, and the next session can't bind that port.
  - **`--idle-exit SECS`:** the session closes after not playing for that long
    (paused, or at the end of the track).
  - **`--port`, `--control-port`:** the Snapcast stream port (1704) and JSON-RPC port
    (port + 1). They must match `-snapcastStream`.
  - dms adds `--paused`: the renderer's play starts the session.
- **What the helper needs at runtime:**
  - **`snapsink`** is built into it, so no installed plugin is needed.
  - **`gst-snapcast-daemon`,** on `PATH` (`make install` puts it into
    `~/.local/bin`) or named in `GST_SNAPCAST_DAEMON`. The helper starts it if it
    isn't running.
  - **`XDG_RUNTIME_DIR`** must be short enough for a Unix socket path (normally
    `/run/user/<uid>`); otherwise the helper fails with a message saying so.
  - **GStreamer's** usual elements (`uridecodebin`, `audioconvert`,
    `audioresample`) and decoders.

  The helper inherits dms's environment.

## Logs

dms logs, per session:

- `snapcast session opening stream=dms-… path=…`: the helper started;
- `snapcast session stream=… feeder="…"`: each line the helper prints, e.g.
  `Session ready: …`, `command pause`, `seek to …`, `end of the track …`,
  `idle: … closing it`, warnings and errors;
- `snapcast session requested … method=HEAD|GET`: a renderer asked;
- `snapcast session closed stream=… exit=…`: the helper ended;
- `snapcast session not opened; serving the media`: the helper didn't get ready.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `flag provided but not defined: -mode` | The helper's options weren't quoted together with it | `-snapcastFeeder "path/session-sender --mode shared --idle-exit 600"` |
| The helper logs `warning: No shared Snapcast server: running an embedded server` | No `--mode shared`, and the daemon wasn't found | Pass `--mode shared`; install the daemon (`make install`) or set `GST_SNAPCAST_DAEMON` |
| The helper logs `error: … shared Snapcast server: cannot start …` | `--mode shared`, and the daemon wasn't found | Install the daemon (`make install`) or set `GST_SNAPCAST_DAEMON` |
| `… socket path … is N bytes, but Unix sockets allow at most 107` | `XDG_RUNTIME_DIR` is too long | Use the normal `/run/user/<uid>` |
| The renderer plays the plain file instead of a session | The renderer doesn't ask (gmrender without `--snapcast-sessions`, or without `snapsrc` installed), or the session didn't open (see dms's log) | Check gmrender's startup line `Asking servers for Snapcast sessions`, and dms's `snapcast session …` lines |

## Status

Proof of concept, not meant for upstream as it is. A possible second `SessionFeeder`
would run ffmpeg into the Snapcast daemon's socket, without GStreamer.
