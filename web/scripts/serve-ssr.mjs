import { createServer } from "node:http"
import { createReadStream, existsSync, statSync } from "node:fs"
import path from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"
import { createRequestListener } from "@react-router/node"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const envPath = path.join(root, ".env")
if (existsSync(envPath)) {
  process.loadEnvFile(envPath)
}

const clientDir = path.join(root, "build/client")
const serverBuildPath = path.join(root, "build/server/index.js")
const port = Number(process.env.PORT || 3000)
const serverURL = process.env.BBSGO_SERVER_URL || process.env.SERVER_URL
if (!serverURL) {
  throw new Error("BBSGO_SERVER_URL is required. Set it in web/.env.")
}

const build = await import(pathToFileURL(serverBuildPath).href)
const frameworkRequestListener = createRequestListener({
  build,
  mode: process.env.NODE_ENV || "production",
})

function contentType(file) {
  const extension = path.extname(file).toLowerCase()
  switch (extension) {
    case ".js":
    case ".mjs":
      return "text/javascript; charset=utf-8"
    case ".css":
      return "text/css; charset=utf-8"
    case ".html":
      return "text/html; charset=utf-8"
    case ".svg":
      return "image/svg+xml"
    case ".png":
      return "image/png"
    case ".jpg":
    case ".jpeg":
      return "image/jpeg"
    case ".webp":
      return "image/webp"
    case ".gif":
      return "image/gif"
    case ".ico":
      return "image/x-icon"
    case ".json":
    case ".map":
      return "application/json; charset=utf-8"
    case ".woff2":
      return "font/woff2"
    case ".woff":
      return "font/woff"
    default:
      return "application/octet-stream"
  }
}

function resolveStaticFile(pathname) {
  let decodedPath
  try {
    decodedPath = decodeURIComponent(pathname)
  } catch {
    return null
  }

  const filePath = path.resolve(clientDir, `.${decodedPath}`)
  const relativePath = path.relative(clientDir, filePath)
  if (
    relativePath.startsWith("..") ||
    path.isAbsolute(relativePath) ||
    relativePath === ""
  ) {
    return null
  }

  try {
    const stats = statSync(filePath)
    return stats.isFile() ? { filePath, stats } : null
  } catch {
    return null
  }
}

function shouldProxyToServer(pathname) {
  return (
    pathname.startsWith("/api/") ||
    pathname.startsWith("/res/") ||
    pathname === "/sitemap.xml"
  )
}

function staticEtag(stats) {
  return `W/"${stats.size.toString(16)}-${Math.trunc(stats.mtimeMs).toString(16)}"`
}

function isStaticNotModified(req, etag, modifiedAt) {
  const ifNoneMatch = req.headers["if-none-match"]
  if (ifNoneMatch) {
    return ifNoneMatch
      .split(",")
      .map((value) => value.trim())
      .includes(etag)
  }

  const ifModifiedSince = req.headers["if-modified-since"]
  if (!ifModifiedSince) return false
  const timestamp = Date.parse(ifModifiedSince)
  return Number.isFinite(timestamp) && modifiedAt.getTime() <= timestamp + 999
}

function serveStatic(req, res, pathname, staticFile) {
  const { filePath, stats } = staticFile
  const etag = staticEtag(stats)
  res.setHeader("Content-Type", contentType(filePath))
  res.setHeader("Content-Length", stats.size)
  res.setHeader("Last-Modified", stats.mtime.toUTCString())
  res.setHeader("ETag", etag)
  res.setHeader(
    "Cache-Control",
    pathname.startsWith("/assets/")
      ? "public, max-age=31536000, immutable"
      : "public, max-age=3600, must-revalidate"
  )

  if (isStaticNotModified(req, etag, stats.mtime)) {
    res.statusCode = 304
    res.removeHeader("Content-Length")
    res.end()
    return
  }
  if (req.method === "HEAD") {
    res.end()
    return
  }

  createReadStream(filePath)
    .on("error", (error) => {
      if (!res.headersSent) {
        res.statusCode = 500
      }
      res.end(error instanceof Error ? error.message : String(error))
    })
    .pipe(res)
}

createServer(async (req, res) => {
  const url = new URL(req.url || "/", `http://${req.headers.host}`)

  if (shouldProxyToServer(url.pathname)) {
    try {
      const upstream = new URL(`${url.pathname}${url.search}`, serverURL)
      const response = await fetch(upstream, {
        method: req.method,
        headers: req.headers,
        body:
          req.method === "GET" || req.method === "HEAD" ? undefined : req,
        duplex: "half",
      })

      res.statusCode = response.status
      response.headers.forEach((value, key) => {
        if (key === "content-encoding" || key === "content-length") {
          return
        }
        res.setHeader(key, value)
      })
      if (response.body) {
        for await (const chunk of response.body) {
          res.write(chunk)
        }
      }
      res.end()
    } catch (error) {
      res.statusCode = 502
      res.end(error instanceof Error ? error.message : String(error))
    }
    return
  }

  const staticFile =
    req.method === "GET" || req.method === "HEAD"
      ? url.pathname === "/"
        ? null
        : resolveStaticFile(url.pathname)
      : null

  if (staticFile) {
    serveStatic(req, res, url.pathname, staticFile)
    return
  }

  try {
    await frameworkRequestListener(req, res)
  } catch (error) {
    res.statusCode = 500
    res.end(error instanceof Error ? error.stack : String(error))
  }
}).listen(port, () => {
  console.log(`web SSR server listening on http://localhost:${port}`)
})
