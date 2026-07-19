export async function launchChromium(chromium) {
  try {
    return await chromium.launch({ channel: "chrome", headless: true, args: ["--no-sandbox"] });
  } catch (channelError) {
    try {
      return await chromium.launch({ headless: true, args: ["--no-sandbox"] });
    } catch (bundledError) {
      if (
        String(bundledError).includes("Executable doesn't exist") ||
        String(bundledError).includes("Please run the following command")
      ) {
        throw new Error(
          "Playwright Chromium is not installed. Run `npm --prefix typescript/browser-integration run install:browsers`.\n" +
            bundledError,
        );
      }
      throw new Error(`launch chrome failed: ${channelError}\nlaunch bundled chromium failed: ${bundledError}`);
    }
  }
}

export async function loadPlaywright() {
  try {
    return await import("playwright");
  } catch (error) {
    if (error?.code === "ERR_MODULE_NOT_FOUND") {
      throw new Error("Playwright is not installed. Run `rtk npm ci --prefix typescript/browser-integration`.");
    }
    throw error;
  }
}
