import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";

const user = {
  id: "user-1",
  email: "admin@anubis.watch",
  name: "Admin",
  role: "admin",
  workspace: "default",
};

const workspaces = [
  { id: "default", name: "Default" },
  { id: "ops", name: "Operations" },
];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("WorkspaceSwitcher", () => {
  beforeEach(() => {
    localStorage.setItem("auth_token", "test-token");
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it("switches to another visible workspace", async () => {
    const onWorkspaceSwitched = vi.fn();
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = new URL(String(input), "http://localhost");
        if (url.pathname === "/api/v1/workspaces") {
          return jsonResponse(workspaces);
        }
        if (
          url.pathname === "/api/v1/auth/workspace" &&
          init?.method === "POST"
        ) {
          expect(init.body).toBe(JSON.stringify({ workspace: "ops" }));
          return jsonResponse({ ...user, workspace: "ops" });
        }
        return jsonResponse({});
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    render(
      <WorkspaceSwitcher
        user={user}
        onWorkspaceSwitched={onWorkspaceSwitched}
      />,
    );

    await screen.findByText("Default");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Switch workspace" }));
    });
    await act(async () => {
      fireEvent.click(
        screen.getByRole("menuitem", { name: "Switch to Operations" }),
      );
    });

    await waitFor(() => {
      expect(onWorkspaceSwitched).toHaveBeenCalledTimes(1);
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/workspace",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows API errors without switching", async () => {
    const onWorkspaceSwitched = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = new URL(String(input), "http://localhost");
        if (url.pathname === "/api/v1/workspaces") {
          return jsonResponse(workspaces);
        }
        if (
          url.pathname === "/api/v1/auth/workspace" &&
          init?.method === "POST"
        ) {
          return jsonResponse({ error: "Forbidden" }, 403);
        }
        return jsonResponse({});
      }),
    );

    render(
      <WorkspaceSwitcher
        user={user}
        onWorkspaceSwitched={onWorkspaceSwitched}
      />,
    );

    await screen.findByText("Default");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Switch workspace" }));
    });
    await act(async () => {
      fireEvent.click(
        screen.getByRole("menuitem", { name: "Switch to Operations" }),
      );
    });

    expect(await screen.findByText("Forbidden")).toBeInTheDocument();
    expect(onWorkspaceSwitched).not.toHaveBeenCalled();
  });
});
