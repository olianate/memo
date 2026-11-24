import { describe, expect, it } from "vitest";
import suite from "../test/body-suite.json";
import { analyzeBody, bodyText, snippetOf } from "./bodyProjection";

describe("body projection conformance", () => {
  for (const tc of suite.cases) {
    it(tc.name, () => {
      expect(bodyText(tc.body)).toBe(tc.bodyText);
      expect(snippetOf(tc.body)).toBe(tc.snippet);
      const analysis = analyzeBody(tc.body);
      expect(analysis.words).toBe(tc.words);
      expect(analysis.characters).toBe(tc.characters);
      expect(analysis.composition).toEqual(tc.composition);
      expect(analysis.todoTotal).toBe(tc.todoTotal);
      expect(analysis.todoDone).toBe(tc.todoDone);
    });
  }
});
