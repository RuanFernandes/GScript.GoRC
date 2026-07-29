import * as React from "react"

import {cn} from "@/lib/utils"

// Skeleton is the shadcn-style loading placeholder: a muted, gently pulsing
// block used to mirror the shape of content that hasn't arrived yet, so a
// surface is never blank-white during an async fetch.
function Skeleton({className, ...props}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("bg-muted animate-pulse rounded-md", className)}
      {...props}
    />
  )
}

export {Skeleton}
