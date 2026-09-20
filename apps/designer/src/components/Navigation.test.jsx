// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0
import React from "react";
import { describe, it, expect, vi, beforeAll, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { Layout } from "./Layout.jsx";

beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute("open");
  };
});
afterEach(() => { cleanup(); localStorage.removeItem("knott-sidebar-collapsed"); });
const mount = () => {
  const onNav = vi.fn();
  render(
    <Layout
      page="dashboard"
      onNav={onNav}
      theme="light"
      onToggleTheme={() => {}}
    >
      <div>Page content</div>
    </Layout>,
  );
  return onNav;
};
describe("Workspace navigation", () => {
  it("persists sidebar collapse and keeps destinations accessible", () => {
    const onNav = mount();
    fireEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
    expect(screen.getByRole('button', { name: 'Expand sidebar' })).toHaveAttribute('aria-expanded', 'false');
    expect(localStorage.getItem('knott-sidebar-collapsed')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Workflows', exact: true }));
    expect(onNav).toHaveBeenCalledWith('workflows');
    cleanup();
    mount();
    expect(screen.getByRole('button', { name: 'Expand sidebar' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }));
    expect(localStorage.getItem('knott-sidebar-collapsed')).toBe('false');
  });
  it("exposes the active page and routes through real buttons", () => {
    const onNav = mount();
    expect(
      screen.getByRole("button", { name: "Overview", exact: true }),
    ).toHaveAttribute("aria-current", "page");
    fireEvent.click(
      screen.getByRole("button", { name: "Executions", exact: true }),
    );
    expect(onNav).toHaveBeenCalledWith("runs");
  });
  it("opens command search with the keyboard, filters pages, and navigates with Enter", () => {
    const onNav = mount();
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    expect(screen.getByRole("dialog")).toHaveAttribute("open");
    const input = screen.getByRole("textbox", { name: "Search pages" });
    fireEvent.change(input, { target: { value: "connector" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onNav).toHaveBeenCalledWith("connectors");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("dismisses navigation with Escape", () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Toggle navigation" }));
    expect(
      screen.getByRole("button", { name: "Toggle navigation" }),
    ).toHaveAttribute("aria-expanded", "true");
    fireEvent.keyDown(window, { key: "Escape" });
    expect(
      screen.getByRole("button", { name: "Toggle navigation" }),
    ).toHaveAttribute("aria-expanded", "false");
  });
});
