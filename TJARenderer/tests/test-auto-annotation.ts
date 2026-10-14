import { calculateInferredHands, generateAutoAnnotations } from "../src/auto-annotation.js";
import { type Annotation, HandType, NoteLocationMap, NoteType } from "../src/primitives.js";
import type { ParsedChart } from "../src/tja-parser.js";

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

function parseSampleText(text: string): { chart: ParsedChart; expectedLabels: string } {
  const lines = text.trim().split("\n");
  const bars: NoteType[][] = [];
  const barParams: { measureRatio: number }[] = [];
  let expectedHands = "";

  for (let i = 0; i < lines.length; i += 3) {
    if (typeof lines[i] === "undefined" || !lines[i]) break;
    const expectedLine = lines[i];
    const notesLine = lines[i + 1];

    const barNotes: NoteType[] = [];
    for (const char of notesLine.replace(/,/g, "")) {
      switch (char) {
        case "1":
          barNotes.push(NoteType.Don);
          break;
        case "2":
          barNotes.push(NoteType.Ka);
          break;
        case "3":
          barNotes.push(NoteType.DonBig);
          break;
        case "4":
          barNotes.push(NoteType.KaBig);
          break;
        case "0":
          barNotes.push(NoteType.None);
          break;
      }
    }
    bars.push(barNotes);
    barParams.push({ measureRatio: 1.0 });
    expectedHands += expectedLine;
  }

  const chart = { bars, barParams } as unknown as ParsedChart;
  return { chart, expectedLabels: expectedHands };
}

const SAMPLE_FULL_ALT = `
r lrl rl rlrl rl
1011201101112011,

 rlrl rl rlrl r 
0211101102111020,

l rlr lr lrlr l 
1021102101211020,

r lrl rl rlrl r 
1021102101211020,

l rlr lr lrlr lr
1011201101112011,

 lrlr lr lrlr l 
0211101102111020,

r lrl rl rlrl r 
1021102101211020,

l rlr l rlrlrlr 
1021102011212110,
`;

const SAMPLE_ALMOST_FULL_ALT = `
r lrl rl rlrl rl
1011201101112011,

 rlrl rl rlrl r 
0211101102111020,

l rlr lr lrlr l 
1021102101211020,

r lrl rl rlrl r 
1021102101211020,

l rlr lr lrlr lr
1011201101112011,

 lrlr lr lrlr l 
0211101102111020,

r lrl rl rlrl r 
1021102101211020,

l rlr l rlrlrlr 
1021102011212110,
`;

const SAMPLE_HALF_ALT = `
r rlr rl rlrl rl
1011201101112011,

 rlrl rl rlrl r 
0211101102111020,

l rlr rl rlrl r 
1021102101211020,

l rlr rl rlrl r 
1021102101211020,

l rlr rl rlrl rl
1011201101112011,

 rlrl rl rlrl r 
0211101102111020,

l rlr rl rlrl r 
1021102101211020,

l rlr r rlrlrlr 
1021102011212110,
`;

const SAMPLE_COOK = `
r rlr rl rlrl rl
1011201101112011,

 rlrl rl rlrl r 
0211101102111020,

r rlr rl rlrl r 
1021102101211020,

r rlr rl rlrl r 
1021102101211020,

r rlr rl rlrl rl
1011201101112011,

 rlrl rl rlrl r 
0211101102111020,

r rlr rl rlrl r 
1021102101211020,

r rlr r rlrlrlr 
1021102011212110,
`;

// Build a single-bar chart from a notes string (1=Don, 2=Ka, 0=rest) at a given BPM, so
// the spacing in time can be controlled for the rolling threshold. A 16-char bar gives
// 16th-note spacing (0.25 beat); at 300 BPM that is a 50ms gap.
function buildDenseChart(notesStr: string, bpm: number): ParsedChart {
  const barNotes: NoteType[] = [];
  for (const char of notesStr) {
    if (char === "1") barNotes.push(NoteType.Don);
    else if (char === "2") barNotes.push(NoteType.Ka);
    else barNotes.push(NoteType.None);
  }
  return { bars: [barNotes], barParams: [{ measureRatio: 1.0, initialBpm: bpm }] } as unknown as ParsedChart;
}

function handString(chart: ParsedChart, inferred: NoteLocationMap<HandType>): string {
  let s = "";
  for (let i = 0; i < chart.bars.length; i++) {
    const bar = chart.bars[i];
    for (let j = 0; j < bar.length; j++) {
      if (bar[j] === NoteType.None) continue;
      const hand = inferred.get({ barIndex: i, charIndex: j });
      s += hand === HandType.R ? "R" : hand === HandType.L ? "L" : "?";
    }
  }
  return s;
}

// Render annotations as a string: "-" for roll taps, otherwise the L/R hand label.
function annotationString(chart: ParsedChart, annotations: NoteLocationMap<Annotation>): string {
  let s = "";
  for (let i = 0; i < chart.bars.length; i++) {
    const bar = chart.bars[i];
    for (let j = 0; j < bar.length; j++) {
      if (bar[j] === NoteType.None) continue;
      const a = annotations.get({ barIndex: i, charIndex: j });
      if (a?.hand === HandType.Roll) s += "-";
      else if (a?.hand === HandType.R) s += "R";
      else if (a?.hand === HandType.L) s += "L";
      else s += "?";
    }
  }
  return s;
}

function testConfiguration(
  chart: ParsedChart,
  expectedLabels: string,
  altThreshold: number,
  resetThreshold: number,
  mainHand: HandType = HandType.R,
) {
  const annotations = new NoteLocationMap<Annotation>();
  const inferred = calculateInferredHands(chart, annotations, altThreshold, resetThreshold, mainHand);

  let actualResult = "";

  for (let i = 0; i < chart.bars.length; i++) {
    const bar = chart.bars[i];
    let line = "";
    for (let j = 0; j < bar.length; j++) {
      const char = bar[j];
      if (char !== NoteType.None) {
        const hand = inferred.get({ barIndex: i, charIndex: j });
        line += hand === HandType.R ? "r" : "l";
      } else {
        line += " ";
      }
    }
    actualResult += line;
  }

  const flatExpected = expectedLabels.replace(/\\n/g, "");
  assert(actualResult === flatExpected, `Expected:\n${flatExpected}\nActual:\n${actualResult}`);
}

try {
  console.log("Testing Auto Annotation...");

  runTest("Auto Annotation - alternation = inf, reset = 0 (100% full alt)", () => {
    const { chart, expectedLabels } = parseSampleText(SAMPLE_FULL_ALT);
    testConfiguration(chart, expectedLabels, Infinity, 0);
  });

  runTest("Auto Annotation - alternation = inf, reset = 4 (almost full alt)", () => {
    const { chart, expectedLabels } = parseSampleText(SAMPLE_ALMOST_FULL_ALT);
    testConfiguration(chart, expectedLabels, Infinity, 4);
  });

  runTest("Auto Annotation - half alt (alternation = inf, reset = 1/12)", () => {
    const { chart, expectedLabels } = parseSampleText(SAMPLE_HALF_ALT);
    testConfiguration(chart, expectedLabels, Infinity, 1 / 12);
  });

  runTest("Auto Annotation - cook (alternation = 1/12, reset = 1/12)", () => {
    const { chart, expectedLabels } = parseSampleText(SAMPLE_COOK);
    testConfiguration(chart, expectedLabels, 1 / 12, 1 / 12);
  });

  runTest("Auto Annotation - left-hand starter mirrors the right starter (full alt)", () => {
    const { chart, expectedLabels } = parseSampleText(SAMPLE_FULL_ALT);
    // With a left starter every hand flips; the result is the exact mirror.
    const mirrored = expectedLabels.replace(/[rl]/g, (c) => (c === "r" ? "l" : "r"));
    testConfiguration(chart, mirrored, Infinity, 0, HandType.L);
  });

  runTest("Rolling - 16th stream at 300 BPM rolls 2-2 (hand does not switch within a swing)", () => {
    const chart = buildDenseChart("1111111111111111", 300);
    const inferred = calculateInferredHands(chart, new NoteLocationMap(), Infinity, 0, HandType.R, 300, 4);
    assert(handString(chart, inferred) === "RRLLRRLLRRLLRRLL", `Got ${handString(chart, inferred)}`);

    const annotations = generateAutoAnnotations(chart, new NoteLocationMap(), Infinity, 0, "full", HandType.R, 300, 4);
    assert(annotationString(chart, annotations) === "R-L-R-L-R-L-R-L-", `Got ${annotationString(chart, annotations)}`);
  });

  runTest("Rolling - applied in partial mode (roll segment fully labeled)", () => {
    const chart = buildDenseChart("1111111111111111", 300);
    const annotations = generateAutoAnnotations(
      chart,
      new NoteLocationMap(),
      Infinity,
      0,
      "partial",
      HandType.R,
      300,
      4,
    );
    assert(annotationString(chart, annotations) === "R-L-R-L-R-L-R-L-", `Got ${annotationString(chart, annotations)}`);
  });

  runTest("Rolling - segment below min length does not roll", () => {
    const chart = buildDenseChart("1110000000000000", 300);
    const inferred = calculateInferredHands(chart, new NoteLocationMap(), Infinity, 0, HandType.R, 300, 4);
    assert(handString(chart, inferred) === "RLR", `Got ${handString(chart, inferred)}`);

    const annotations = generateAutoAnnotations(chart, new NoteLocationMap(), Infinity, 0, "full", HandType.R, 300, 4);
    assert(annotationString(chart, annotations) === "RLR", `Got ${annotationString(chart, annotations)}`);
  });

  runTest("Rolling - spacing wider than threshold does not roll (16ths at 150 BPM)", () => {
    const chart = buildDenseChart("1111111111111111", 150);
    const inferred = calculateInferredHands(chart, new NoteLocationMap(), Infinity, 0, HandType.R, 300, 4);
    assert(handString(chart, inferred) === "RLRLRLRLRLRLRLRL", `Got ${handString(chart, inferred)}`);
  });

  runTest("Rolling - disabled when thresholds omitted (matches full alternation baseline)", () => {
    const chart = buildDenseChart("1111111111111111", 300);
    const inferred = calculateInferredHands(chart, new NoteLocationMap(), Infinity, 0, HandType.R);
    assert(handString(chart, inferred) === "RLRLRLRLRLRLRLRL", `Got ${handString(chart, inferred)}`);
  });

  runTest("Rolling - colouring baseline (no roll params) agrees with auto-annotated hands", () => {
    const chart = buildDenseChart("1111111111111111", 300);
    const annotations = generateAutoAnnotations(chart, new NoteLocationMap(), Infinity, 0, "full", HandType.R, 300, 4);
    // The layout computes the match/mismatch colouring baseline without roll thresholds, so
    // it must lean on the stored Roll annotations to track the swing. Otherwise the non-roll
    // taps in a rolled segment disagree with the baseline and get flagged red.
    const baseline = calculateInferredHands(chart, annotations, Infinity, 0, HandType.R);
    for (let j = 0; j < chart.bars[0].length; j++) {
      const id = { barIndex: 0, charIndex: j };
      const a = annotations.get(id);
      if (!a || a.hand === HandType.Roll) continue;
      assert(baseline.get(id) === a.hand, `Baseline ${baseline.get(id)} != annotation ${a.hand} at ${j}`);
    }
  });

  runTest("Rolling - odd trailing note is a normal alternated tap", () => {
    const chart = buildDenseChart("1111100000000000", 300);
    const inferred = calculateInferredHands(chart, new NoteLocationMap(), Infinity, 0, HandType.R, 300, 4);
    assert(handString(chart, inferred) === "RRLLR", `Got ${handString(chart, inferred)}`);

    const annotations = generateAutoAnnotations(chart, new NoteLocationMap(), Infinity, 0, "full", HandType.R, 300, 4);
    assert(annotationString(chart, annotations) === "R-L-R", `Got ${annotationString(chart, annotations)}`);
  });

  console.log("\nAll auto annotation tests passed.\n");
} catch (e) {
  if (e instanceof Error) {
    console.error(`\nFATAL: ${e.message}\n`);
  }
  process.exit(1);
}
