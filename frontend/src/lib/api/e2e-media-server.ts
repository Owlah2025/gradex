import http from "http";
import fs from "fs";
import path from "path";
import net from "net";
import { execFileSync } from "child_process";

export const FIXTURE_DIR = "/var/tmp/gradex-s5-media-fixture";

export function ensureMediaFixture(): void {
  if (!fs.existsSync(FIXTURE_DIR)) {
    fs.mkdirSync(FIXTURE_DIR, { recursive: true });
  }

  const masterPath = path.join(FIXTURE_DIR, "master.m3u8");
  if (fs.existsSync(masterPath)) return;

  console.log("[E2E Media Server] Generating deterministic HLS test fixture with ffmpeg...");

  // Generate 720p variant (30 seconds)
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc=size=1280x720:rate=30",
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=440:sample_rate=44100",
      "-t",
      "30",
      "-c:v",
      "libx264",
      "-preset",
      "ultrafast",
      "-pix_fmt",
      "yuv420p",
      "-g",
      "30",
      "-c:a",
      "aac",
      "-b:a",
      "128k",
      "-f",
      "hls",
      "-hls_time",
      "5",
      "-hls_list_size",
      "0",
      path.join(FIXTURE_DIR, "720p.m3u8"),
    ],
    { stdio: "ignore" }
  );

  // Generate 360p variant (30 seconds)
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc=size=640x360:rate=30",
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=440:sample_rate=44100",
      "-t",
      "30",
      "-c:v",
      "libx264",
      "-preset",
      "ultrafast",
      "-pix_fmt",
      "yuv420p",
      "-g",
      "30",
      "-c:a",
      "aac",
      "-b:a",
      "128k",
      "-f",
      "hls",
      "-hls_time",
      "5",
      "-hls_list_size",
      "0",
      path.join(FIXTURE_DIR, "360p.m3u8"),
    ],
    { stdio: "ignore" }
  );

  // Create master playlist
  const masterContent = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720,NAME="720p"
720p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360,NAME="360p"
360p.m3u8
`;
  fs.writeFileSync(masterPath, masterContent, "utf-8");
}

export type LocalMediaServer = {
  server: http.Server;
  port: number;
  origin: string;
  close: () => Promise<void>;
};

export async function startLocalMediaServer(): Promise<LocalMediaServer> {
  ensureMediaFixture();

  return new Promise((resolve, reject) => {
    const server = http.createServer((req, res) => {
      // CORS headers
      res.setHeader("Access-Control-Allow-Origin", "*");
      res.setHeader("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS");
      res.setHeader("Access-Control-Allow-Headers", "*");

      if (req.method === "OPTIONS") {
        res.writeHead(204);
        res.end();
        return;
      }

      const reqUrl = req.url || "/";
      const urlPath = reqUrl.split("?")[0];
      const filename = path.basename(urlPath);
      const filePath = path.join(FIXTURE_DIR, filename);

      if (!fs.existsSync(filePath) || !fs.statSync(filePath).isFile()) {
        res.writeHead(404, { "Content-Type": "text/plain" });
        res.end("Not Found");
        return;
      }

      let contentType = "application/octet-stream";
      if (filename.endsWith(".m3u8")) {
        contentType = "application/vnd.apple.mpegurl";
      } else if (filename.endsWith(".ts")) {
        contentType = "video/mp2t";
      }

      res.writeHead(200, {
        "Content-Type": contentType,
        "Cache-Control": "no-cache",
      });

      fs.createReadStream(filePath).pipe(res);
    });

    server.listen(0, "127.0.0.1", () => {
      const addr = server.address() as net.AddressInfo;
      const port = addr.port;
      const origin = `http://127.0.0.1:${port}`;

      resolve({
        server,
        port,
        origin,
        close: () =>
          new Promise<void>((resClose) => {
            server.close(() => resClose());
          }),
      });
    });

    server.on("error", (err) => reject(err));
  });
}
