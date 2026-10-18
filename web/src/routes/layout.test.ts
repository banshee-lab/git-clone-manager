import { render, screen } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import LayoutHost from "../test/LayoutHost.svelte";

describe("application layout", () => {
  beforeEach(() => {
    localStorage.setItem("runwisp:theme", "auto");
  });

  it("renders the workspace shell and persists theme toggle choices", async () => {
    const user = userEvent.setup();
    render(LayoutHost);

    expect(
      screen.getByRole("link", { name: "GitSync home" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("complementary", { name: "Repository navigation" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("repository-list-slot")).toBeInTheDocument();
    expect(screen.getByText("Test route outlet")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Theme" }));
    await user.click(screen.getByRole("menuitem", { name: "Light" }));
    expect(localStorage.getItem("runwisp:theme")).toBe("light");
    expect(document.documentElement).not.toHaveClass("dark");

    await user.click(screen.getByRole("button", { name: "Theme" }));
    await user.click(screen.getByRole("menuitem", { name: "Dark" }));
    expect(localStorage.getItem("runwisp:theme")).toBe("dark");
    expect(document.documentElement).toHaveClass("dark");

    await user.click(screen.getByRole("button", { name: "Theme" }));
    await user.click(screen.getByRole("menuitem", { name: "Auto" }));
    expect(localStorage.getItem("runwisp:theme")).toBe("auto");
    expect(document.documentElement).not.toHaveClass("dark");
  });
});
