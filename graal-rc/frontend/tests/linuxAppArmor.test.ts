import assert from "node:assert/strict"
import {readFile} from "node:fs/promises"
import {fileURLToPath} from "node:url"
import test from "node:test"

const projectRoot = fileURLToPath(new URL("../../", import.meta.url))

test("Linux packaging grants WebKitGTK user namespaces to the installed GoRC binary", async () => {
  const profile = await readFile(`${projectRoot}/build/linux/apparmor/graal-rc`, "utf8")
  const nfpm = await readFile(`${projectRoot}/build/linux/nfpm/nfpm.yaml`, "utf8")
  const postinstall = await readFile(`${projectRoot}/build/linux/nfpm/scripts/postinstall.sh`, "utf8")
  const preremove = await readFile(`${projectRoot}/build/linux/nfpm/scripts/preremove.sh`, "utf8")

  assert.match(profile, /profile graal-rc \/usr\/local\/bin\/graal-rc/)
  assert.match(profile, /userns,/)
  assert.match(profile, /include if exists <local\/graal-rc>/)
  assert.match(nfpm, /build\/linux\/apparmor\/graal-rc/)
  assert.match(nfpm, /preremove: .*preremove\.sh/)
  assert.match(postinstall, /apparmor_parser -r \/etc\/apparmor\.d\/graal-rc/)
  assert.match(preremove, /apparmor_parser -R \/etc\/apparmor\.d\/graal-rc/)
})
