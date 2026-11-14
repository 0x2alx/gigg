import { describe, expect, it } from "vitest";
import { parseTools, runBody, validModel, validTool } from "./runsettings";

describe("run settings", () => {
  it("accepts model names and refuses anything that isn't one argument", () => {
    for (const ok of ["", "opus", "claude-opus-5-5", "claude-opus-5-5[1m]", "us.anthropic.claude:1"]) expect(validModel(ok)).toBe(true);
    for (const bad of ["-x", "opus; id", "a b", "$(id)", "x".repeat(65)]) expect(validModel(bad)).toBe(false);
  });
  it("sends only what is set", () => {
    expect(runBody({ model: "", effort: "", always_run: false, allowed_tools: [] })).toEqual({});
    expect(runBody({ model: "sonnet", effort: "high", always_run: true, allowed_tools: ["Read"] })).toEqual({ model: "sonnet", effort: "high", always_run: true, allowed_tools: ["Read"] });
  });
  it("tool rules: what the server and the runner accept", () => {
    for (const ok of ["Read", "Bash(git status *)", "Edit(//srv/shared/reports/**)", "mcp__gigg__get_task", "WebFetch(domain:example.com)", 'Bash(curl -s "api.example.com/v1/forecast?city=Paris,FR")']) expect(validTool(ok)).toBe(true);
    for (const bad of ["-p", "--allowedTools", "Read,Write", "Bash(a(b))", "Read Write", "Bash(x\ny)", "Bash()", "(x)", "Read)", "x".repeat(501)]) expect(validTool(bad)).toBe(false);
  });
  it("parses one rule per line, drops blanks and repeats, names the first bad one", () => {
    expect(parseTools(" Read\n\nBash(git status *)\nRead\n")).toEqual({ rules: ["Read", "Bash(git status *)"], bad: null });
    expect(parseTools("Read\nRead, Write")).toEqual({ rules: ["Read"], bad: "Read, Write" });
    expect(parseTools("Bash(curl x?a=1,2)").bad).toBe(null);
  });
});
