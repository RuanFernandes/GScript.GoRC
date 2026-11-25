// Marks a private conversation read only while the user can see it.
interface WatchPmReadOptions {
  unread: number
  isVisible: () => boolean
  isFocused: () => boolean
  // subscribe calls check whenever the window may have come into view.
  subscribe: (check: () => void) => () => void
  markRead: () => PromiseLike<unknown>
}

// watchPmRead applies to one snapshot of the unread count. It only calls
// markRead on start or on a window event, never because a request settled, so a
// failing request cannot loop.
export function watchPmRead(options: WatchPmReadOptions): () => void {
  if (options.unread <= 0) return () => {}
  let pending = false
  const check = () => {
    if (pending || !options.isVisible() || !options.isFocused()) return
    pending = true
    const done = () => { pending = false }
    options.markRead().then(done, done)
  }
  const unsubscribe = options.subscribe(check)
  check()
  return unsubscribe
}
