import { lazy, Suspense, useEffect } from "react";
import { Navigate, Outlet, Route, Routes } from "react-router-dom";
import { useAuth } from "./api/hooks";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { Layout } from "./components/Layout";
import { WebSocketProvider } from "./hooks/useWebSocket";
import { applyTheme, useThemeStore } from "./stores/themeStore";

const Dashboard = lazy(() =>
	import("./pages/Dashboard").then((module) => ({ default: module.Dashboard })),
);
const Souls = lazy(() =>
	import("./pages/Souls").then((module) => ({ default: module.Souls })),
);
const SoulDetail = lazy(() =>
	import("./pages/SoulDetail").then((module) => ({
		default: module.SoulDetail,
	})),
);
const SoulEdit = lazy(() =>
	import("./pages/SoulEdit").then((module) => ({ default: module.SoulEdit })),
);
const Judgments = lazy(() =>
	import("./pages/Judgments").then((module) => ({ default: module.Judgments })),
);
const Alerts = lazy(() =>
	import("./pages/Alerts").then((module) => ({ default: module.Alerts })),
);
const Journeys = lazy(() =>
	import("./pages/Journeys").then((module) => ({ default: module.Journeys })),
);
const Cluster = lazy(() =>
	import("./pages/Cluster").then((module) => ({ default: module.Cluster })),
);
const StatusPages = lazy(() =>
	import("./pages/StatusPages").then((module) => ({
		default: module.StatusPages,
	})),
);
const Dashboards = lazy(() =>
	import("./pages/Dashboards").then((module) => ({
		default: module.Dashboards,
	})),
);
const DashboardDetail = lazy(() =>
	import("./pages/DashboardDetail").then((module) => ({
		default: module.DashboardDetail,
	})),
);
const Incidents = lazy(() =>
	import("./pages/Incidents").then((module) => ({ default: module.Incidents })),
);
const Maintenance = lazy(() =>
	import("./pages/Maintenance").then((module) => ({
		default: module.Maintenance,
	})),
);
const Reports = lazy(() =>
	import("./pages/Reports").then((module) => ({ default: module.Reports })),
);
const Settings = lazy(() =>
	import("./pages/Settings").then((module) => ({ default: module.Settings })),
);
const Login = lazy(() =>
	import("./pages/Login").then((module) => ({ default: module.Login })),
);
const ForgotPassword = lazy(() =>
	import("./pages/PasswordReset").then((module) => ({ default: module.ForgotPassword })),
);
const ResetPassword = lazy(() =>
	import("./pages/PasswordReset").then((module) => ({ default: module.ResetPassword })),
);
const AcceptInvite = lazy(() =>
	import("./pages/PasswordReset").then((module) => ({ default: module.AcceptInvite })),
);
const ForcedPasswordChange = lazy(() =>
	import("./components/ChangePasswordForm").then((module) => ({
		default: module.ForcedPasswordChange,
	})),
);
const NotFound = lazy(() =>
	import("./pages/NotFound").then((module) => ({ default: module.NotFound })),
);

function ProtectedRoute() {
	const { isAuthenticated, loading, user } = useAuth();

	if (loading) {
		return (
			<div
				className="min-h-screen bg-gray-950 flex items-center justify-center"
				role="status"
				aria-label="Loading"
			>
				<div className="w-8 h-8 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
				<span className="sr-only">Loading...</span>
			</div>
		);
	}

	if (!isAuthenticated) {
		return <Navigate to="/login" replace />;
	}

	// An account created with an initial password may do nothing else until
	// it sets its own (the server enforces this too).
	if (user?.must_change_password) {
		return <ForcedPasswordChange />;
	}

	return <Outlet />;
}

function ThemeController() {
	const theme = useThemeStore((state) => state.theme);

	useEffect(() => {
		applyTheme(theme);

		if (
			theme !== "system" ||
			typeof window === "undefined" ||
			typeof window.matchMedia !== "function"
		) {
			return;
		}

		const media = window.matchMedia("(prefers-color-scheme: dark)");
		const updateSystemTheme = () => applyTheme("system");

		media.addEventListener?.("change", updateSystemTheme);
		return () => media.removeEventListener?.("change", updateSystemTheme);
	}, [theme]);

	return null;
}

function App() {
	return (
		<WebSocketProvider>
			<ThemeController />
			<a href="#main-content" className="skip-link">
				Skip to main content
			</a>
			<ErrorBoundary>
				<Suspense
					fallback={
						<div
							className="min-h-screen bg-gray-950 flex items-center justify-center"
							role="status"
							aria-label="Loading page"
						>
							<div className="w-8 h-8 border-2 border-amber-500/30 border-t-amber-500 rounded-full animate-spin" />
							<span className="sr-only">Loading page...</span>
						</div>
					}
				>
					<Routes>
						<Route path="/login" element={<Login />} />
						<Route path="/forgot-password" element={<ForgotPassword />} />
						<Route path="/reset-password" element={<ResetPassword />} />
						<Route path="/accept-invite" element={<AcceptInvite />} />
						<Route element={<ProtectedRoute />}>
							<Route element={<Layout />}>
								<Route path="/" element={<Dashboard />} />
								<Route path="/souls" element={<Souls />} />
								<Route path="/souls/:id" element={<SoulDetail />} />
								<Route path="/souls/:id/edit" element={<SoulEdit />} />
								<Route path="/judgments" element={<Judgments />} />
								<Route path="/alerts" element={<Alerts />} />
								<Route path="/incidents" element={<Incidents />} />
								<Route path="/maintenance" element={<Maintenance />} />
								<Route path="/reports" element={<Reports />} />
								<Route path="/journeys" element={<Journeys />} />
								<Route path="/cluster" element={<Cluster />} />
								<Route path="/status-pages" element={<StatusPages />} />
								<Route path="/dashboards" element={<Dashboards />} />
								<Route path="/dashboards/:id" element={<DashboardDetail />} />
								<Route path="/dashboards/new" element={<DashboardDetail />} />
								<Route path="/settings" element={<Settings />} />
								<Route path="*" element={<NotFound />} />
							</Route>
						</Route>
					</Routes>
				</Suspense>
			</ErrorBoundary>
		</WebSocketProvider>
	);
}

export default App;
