import type {FileBrowserFolder} from "@/types"

export interface FileBrowserTreeNode {
  name: string
  path: string
  rights?: string
  globs?: string[]
  children: FileBrowserTreeNode[]
}

// buildFileBrowserTree turns the flat access patterns returned by the server
// into the shared tree used by the browser and the backup selector. The backup
// selector may opt into a synthetic root so root-level glob access is selectable.
export function buildFileBrowserTree(folders: FileBrowserFolder[], includeRoot = false): FileBrowserTreeNode[] {
  const root: FileBrowserTreeNode[] = []
  const rootNode: FileBrowserTreeNode | null = includeRoot ? {name: "/", path: "", children: []} : null
  const treeRoot = rootNode ? rootNode.children : root
  let rootAccessible = false
  for (const folder of folders) {
    const segments = (folder.pattern ?? "").split("/").filter(Boolean)
    let glob = ""
    if (segments.length > 0 && segments[segments.length - 1].includes("*")) {
      glob = segments.pop() as string
    }
    const folderPath = segments.join("/")
    if (!folderPath) {
      if (rootNode && glob) {
        rootAccessible = true
        if (folder.rights) rootNode.rights = folder.rights
        rootNode.globs ??= []
        if (!rootNode.globs.includes(glob)) rootNode.globs.push(glob)
      }
      continue
    }

    const parts = folderPath.split("/")
    let level = treeRoot
    let accumulated = ""
    parts.forEach((part, index) => {
      accumulated = accumulated ? `${accumulated}/${part}` : part
      let node = level.find((candidate) => candidate.path === accumulated)
      if (!node) {
        node = {name: `${part}/`, path: accumulated, children: []}
        level.push(node)
      }
      if (index === parts.length - 1) {
        if (folder.rights) node.rights = folder.rights
        if (glob) {
          node.globs ??= []
          if (!node.globs.includes(glob)) node.globs.push(glob)
        }
      }
      level = node.children
    })
  }

  const sortNodes = (nodes: FileBrowserTreeNode[]) => {
    nodes.sort((a, b) =>
      a.children.length === 0 === (b.children.length === 0)
        ? a.name.localeCompare(b.name)
        : a.children.length === 0
          ? 1
          : -1,
    )
    nodes.forEach((node) => sortNodes(node.children))
  }
  sortNodes(treeRoot)
  return rootNode ? (rootAccessible ? [rootNode] : rootNode.children) : root
}

export function flattenFileBrowserTree(nodes: FileBrowserTreeNode[]): FileBrowserTreeNode[] {
  const result: FileBrowserTreeNode[] = []
  const visit = (items: FileBrowserTreeNode[]) => {
    for (const node of items) {
      result.push(node)
      visit(node.children)
    }
  }
  visit(nodes)
  return result
}
