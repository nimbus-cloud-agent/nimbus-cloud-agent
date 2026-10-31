// stdout is a pipe under the Go supervisor. process.exit() does not flush pending
// writes, so every final protocol response must finish writing before exit.
export async function writeJSON(value) {
  await new Promise((resolve, reject) => {
    process.stdout.write(JSON.stringify(value) + "\n", (error) => {
      if (error) reject(error);
      else resolve();
    });
  });
}
