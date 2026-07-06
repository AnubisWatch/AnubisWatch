import "@testing-library/jest-dom";
import { cleanup } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, vi } from "vitest";

// Extend global for TypeScript
declare global {
	var ResizeObserver: typeof ResizeObserver;
}

const TEST_RECT = {
	width: 400,
	height: 300,
	top: 0,
	left: 0,
	bottom: 300,
	right: 400,
	x: 0,
	y: 0,
};

// Mock ResizeObserver for Recharts ResponsiveContainer
class ResizeObserverMock {
	observe(target?: Element) {
		if (target && "clientWidth" in target) {
			Object.defineProperty(target, "clientWidth", {
				configurable: true,
				value: TEST_RECT.width,
			});
			Object.defineProperty(target, "clientHeight", {
				configurable: true,
				value: TEST_RECT.height,
			});
		}
	}
	unobserve() {}
	disconnect() {}
}
globalThis.ResizeObserver =
	ResizeObserverMock as unknown as typeof ResizeObserver;

vi.mock("recharts", async () => {
	const actual = await vi.importActual<typeof import("recharts")>("recharts");
	return {
		...actual,
		ResponsiveContainer: ({
			children,
		}: {
			children?:
				| ReturnType<typeof createElement>
				| ReturnType<typeof createElement>[]
				| string
				| number
				| boolean
				| null;
		}) =>
			createElement(
				"div",
				{
					className: "recharts-responsive-container",
					style: {
						width: `${TEST_RECT.width}px`,
						height: `${TEST_RECT.height}px`,
					},
					"data-testid": "responsive-container",
				},
				children,
			),
	};
});

Object.defineProperties(HTMLElement.prototype, {
	clientWidth: { configurable: true, get: () => TEST_RECT.width },
	clientHeight: { configurable: true, get: () => TEST_RECT.height },
	offsetWidth: { configurable: true, get: () => TEST_RECT.width },
	offsetHeight: { configurable: true, get: () => TEST_RECT.height },
});

Element.prototype.getBoundingClientRect = () =>
	({
		...TEST_RECT,
		toJSON: () => ({}),
	}) as DOMRect;

// Cleanup after each test
afterEach(() => {
	cleanup();
});
