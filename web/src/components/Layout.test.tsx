import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { Layout } from "../components/Layout";

// Mock useAuth hook
vi.mock("../api/hooks", () => ({
  useAuth: () => ({
    user: {
      name: "Test User",
      email: "test@anubis.watch",
      workspace: "default",
    },
    logout: vi.fn(),
  }),
}));

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

// Mock child components
const MockDashboard = () => (
  <div data-testid="dashboard-content">Dashboard Content</div>
);

describe("Layout", () => {
  it("renders sidebar and header", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse([])),
    );

    const { container } = render(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<MockDashboard />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());

    // Check main layout elements
    const main = container.querySelector("main");
    expect(main).toBeInTheDocument();

    const header = container.querySelector("header");
    expect(header).toBeInTheDocument();

    const aside = container.querySelector("aside");
    expect(aside).toBeInTheDocument();

    expect(screen.getByTestId("dashboard-content")).toBeInTheDocument();
  });

  it("opens and closes the mobile navigation drawer", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse([])),
    );

    const { container } = render(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<MockDashboard />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());

    const aside = container.querySelector("aside");
    expect(aside?.className).toContain("-translate-x-full");

    fireEvent.click(screen.getByLabelText("Open navigation menu"));
    expect(aside?.className).toContain("translate-x-0");

    fireEvent.click(screen.getByLabelText("Close navigation menu"));
    expect(aside?.className).toContain("-translate-x-full");
  });
});
