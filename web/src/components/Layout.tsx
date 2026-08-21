import { useState } from "react";
import { Outlet } from "react-router-dom";
import { Header } from "./Header";
import { Sidebar } from "./Sidebar";

export function Layout() {
	const [sidebarOpen, setSidebarOpen] = useState(false);

	return (
		<div className="flex min-h-screen bg-[var(--bg-primary)] relative text-[var(--text-primary)]">
			<div className="fixed inset-0 pointer-events-none bg-[radial-gradient(circle_at_top_left,rgba(212,175,55,0.08),transparent_28%),radial-gradient(circle_at_bottom_right,rgba(20,184,166,0.06),transparent_24%)]" />
			<div className="fixed inset-0 hieroglyph-pattern opacity-[0.08] pointer-events-none" />

			<Sidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} />
			<div className="flex-1 flex min-h-screen flex-col relative z-10">
				<Header onMenuClick={() => setSidebarOpen(true)} />
				<main
					id="main-content"
					className="flex-1 overflow-auto px-4 py-5 sm:px-6 sm:py-6 lg:px-8"
				>
					<div className="mx-auto w-full max-w-7xl">
						<Outlet />
					</div>
				</main>
			</div>
		</div>
	);
}
