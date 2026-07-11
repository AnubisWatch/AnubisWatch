import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api/client";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { AUTH_SESSION_CHANGED_EVENT, type AuthSessionChange } from "../api/authEvents";

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
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it("switches to another visible workspace", async () => {
    const onWorkspaceSwitched = vi.fn();
    const sessionChanges: AuthSessionChange[] = [];
    const onSessionChange = (event: Event) => {
      sessionChanges.push((event as CustomEvent<AuthSessionChange>).detail);
    };
    window.addEventListener(AUTH_SESSION_CHANGED_EVENT, onSessionChange, { once: true });
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
    expect(sessionChanges).toEqual([
      { state: "authenticated", user: { ...user, workspace: "ops" } },
    ]);
    expect(localStorage.getItem("auth_user")).toBeNull();
  });

  it("renders nothing without a user and clears workspaces", async () => {
    const get = vi.spyOn(api, "get");
    const { container } = render(<WorkspaceSwitcher user={null} />);
    expect(container).toBeEmptyDOMElement();
    expect(get).not.toHaveBeenCalled();
  });

  it("handles invalid workspace payloads and load failures", async () => {
    vi.spyOn(api, "get").mockResolvedValueOnce({} as never);
    const view = render(<WorkspaceSwitcher user={user} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Switch workspace" })).toBeDisabled());
    view.unmount();

    vi.spyOn(api, "get").mockRejectedValueOnce("bad");
    const second = render(<WorkspaceSwitcher user={user} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Switch workspace" })).toBeDisabled());
    second.unmount();

    vi.spyOn(api, "get").mockRejectedValueOnce(new Error("load failed"));
    render(<WorkspaceSwitcher user={user} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Switch workspace" })).toBeDisabled());
  });

  it("keeps the menu open for pointer events inside the switcher", async () => {
    vi.spyOn(api, "get").mockResolvedValue(workspaces);
    render(<WorkspaceSwitcher user={user} />);
    const trigger = await screen.findByRole("button", { name: "Switch workspace" });
    fireEvent.click(trigger);
    fireEvent.pointerDown(trigger);
    expect(screen.getByRole("menu")).toBeInTheDocument();
  });

  it("uses workspace ids when names are empty", async () => {
    vi.spyOn(api, "get").mockResolvedValue([{ id: "default", name: "" }, { id: "ops", name: "" }]);
    render(<WorkspaceSwitcher user={user} />);
    fireEvent.click(await screen.findByRole("button", { name: "Switch workspace" }));
    expect(screen.getByRole("menuitem", { name: "Switch to ops" })).toBeInTheDocument();
  });

  it("closes the menu for outside pointer events and current workspace selections", async () => {
    vi.spyOn(api, "get").mockResolvedValue(workspaces);
    render(<WorkspaceSwitcher user={user} />);
    const trigger = await screen.findByRole("button", { name: "Switch workspace" });
    fireEvent.click(trigger);
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole("menuitem", { name: "Switch to Default" }));
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("reloads after a successful switch when no callback is supplied", async () => {
    vi.spyOn(api, "get").mockResolvedValue(workspaces);
    vi.spyOn(api, "post").mockResolvedValue({ ...user, workspace: "ops" });
    const original = window.location;
    const reload = vi.fn();
    Object.defineProperty(window, "location", { configurable: true, value: { ...original, reload } });
    render(<WorkspaceSwitcher user={user} />);
    fireEvent.click(await screen.findByRole("button", { name: "Switch workspace" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Switch to Operations" }));
    await waitFor(() => expect(reload).toHaveBeenCalled());
    Object.defineProperty(window, "location", { configurable: true, value: original });
  });

  it("shows a fallback for non-Error switch failures", async () => {
    vi.spyOn(api, "get").mockResolvedValue(workspaces);
    vi.spyOn(api, "post").mockRejectedValue("bad");
    render(<WorkspaceSwitcher user={user} />);
    fireEvent.click(await screen.findByRole("button", { name: "Switch workspace" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Switch to Operations" }));
    expect(await screen.findByText("Failed to switch workspace")).toBeInTheDocument();
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
