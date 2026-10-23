import { snapForDevicePixel } from "../src/drawing-utils.js";

function runTest(name: string, fn: () => void) {
  try {
    console.log(`\n--- ${name} ---`);
    fn();
    console.log("PASS");
  } catch (e) {
    if (e instanceof Error) {
      console.error(`FAIL: ${e.message}`);
    } else {
      console.error(`FAIL: ${e}`);
    }
    process.exit(1);
  }
}

function assert(condition: boolean, message: string) {
  if (!condition) {
    throw new Error(message);
  }
}

try {
  console.log("Testing Pixel Snapping Logic...");

  runTest("snapForDevicePixel calculation", () => {
    const dpr = 2; // Retina display
    const lineWidth = 1.5; // Logical width
    const x = 100.1; // Some coordinate not on pixel boundary

    // deviceBorderW = round(1.5 * 2) = 3 (odd)
    // expected = (round(100.1 * 2) + 0.5) / 2 = (200 + 0.5) / 2 = 100.25
    const snapped = snapForDevicePixel(x, lineWidth, dpr);
    assert(snapped === 100.25, `Expected 100.25, got ${snapped}`);
  });

  runTest("Misalignment check", () => {
    const dprs = [1, 1.5, 2, 2.5, 3];
    const barBorderWidth = 1.5;
    const x = 10.333333;

    for (const dpr of dprs) {
      const expected = snapForDevicePixel(x, barBorderWidth, dpr);
      const actualBuggy = x;

      assert(actualBuggy !== expected, `At dpr=${dpr}, raw X should differ from snapped`);

      if (dpr === 2) {
        assert(expected === 10.75, `At dpr=2, expected 10.75, got ${expected}`);
      }
    }
  });

  console.log("\nAll pixel snapping tests passed.\n");
} catch (e) {
  if (e instanceof Error) {
    console.error(`\nFATAL: ${e.message}\n`);
  }
  process.exit(1);
}
