// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0
import React from "react";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import Dashboard from "./Dashboard.jsx";
import { stats, runs } from "../lib/api.js";
vi.mock("../lib/api.js", () => ({
  stats: { get: vi.fn() },
  runs: { list: vi.fn() },
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
describe("Operations overview data states", () => {
  it("shows unavailable data and lets operators retry without displaying false zeroes", async () => {
    stats.get
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValue({ total_workflows: 4, daily: [] });
    runs.list.mockResolvedValue({ data: [] });
    render(<Dashboard onNav={() => {}} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Unable to refresh",
    );
    expect(
      screen.getByRole("button", { name: /Total workflows/ }),
    ).toHaveTextContent("—");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(
      await screen.findByRole("button", { name: /Total workflows 4/ }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("routes the create action to the designer and reports empty execution history", async () => {
    stats.get.mockResolvedValue({ total_workflows: 0, daily: [] });
    runs.list.mockResolvedValue({ data: [] });
    const onNav = vi.fn();
    render(<Dashboard onNav={onNav} />);
    expect(
      await screen.findByText("Ready for your first run"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Create workflow" }));
    expect(onNav).toHaveBeenCalledWith("designer");
  });
});
