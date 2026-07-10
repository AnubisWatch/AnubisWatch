import { ArrowRight, Eye, EyeOff, Lock, Mail } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";

const AnkhIcon = () => (
	<svg
		className="h-5 w-5 text-[var(--accent-primary)]"
		viewBox="0 0 24 24"
		fill="currentColor"
		aria-hidden="true"
	>
		<path d="M8 3C8 1.9 8.9 1 10 1C11.1 1 12 1.9 12 3V8H16C17.1 8 18 8.9 18 10C18 11.1 17.1 12 16 12H12V21C12 22.1 11.1 23 10 23C8.9 23 8 22.1 8 21V12H4C2.9 12 2 11.1 2 10C2 8.9 2.9 8 4 8H8V3Z" />
	</svg>
);

export function Login() {
	const navigate = useNavigate();
	const [email, setEmail] = useState("");
	const [password, setPassword] = useState("");
	const [showPassword, setShowPassword] = useState(false);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState("");

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		setLoading(true);
		setError("");

		try {
			await api.post<{
				user: { id: string; email: string; name: string };
			}>("/auth/login", {
				email,
				password,
			});
			// The server sets an HttpOnly auth_token cookie. All subsequent
			// API calls include it via credentials: 'include'.
			navigate("/");
		} catch (err) {
			setError(
				err instanceof Error
					? err.message
					: "The gods have rejected your offering",
			);
		} finally {
			setLoading(false);
		}
	};

	return (
		<div className="relative flex min-h-screen items-center justify-center bg-[var(--bg-primary)] px-4 py-10 text-[var(--text-primary)]">
			<div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top_left,rgba(212,175,55,0.08),transparent_26%),radial-gradient(circle_at_bottom_right,rgba(20,184,166,0.05),transparent_24%)]" />
			<div className="pointer-events-none absolute inset-0 hieroglyph-pattern opacity-[0.08]" />

			<div className="relative z-10 w-full max-w-md">
				<div className="mb-8 text-center">
					<div className="mb-5 inline-flex h-20 w-20 items-center justify-center rounded-2xl border border-[var(--border-strong)] bg-[var(--bg-elevated)] shadow-[var(--shadow-soft)]">
						<img
							src="/jackal-logo.svg"
							alt="AnubisWatch"
							className="h-12 w-12"
						/>
					</div>
					<div className="mb-3 flex items-center justify-center gap-2 text-[var(--accent-primary)]">
						<AnkhIcon />
						<span className="text-xs font-medium uppercase tracking-[0.18em] text-[var(--text-muted)]">
							Hall of Ma&apos;at
						</span>
					</div>
					<h1 className="text-3xl font-semibold tracking-tight text-[var(--text-primary)]">
						AnubisWatch
					</h1>
					<p className="mt-2 text-sm text-[var(--text-secondary)]">
						Monitor your realm with a calmer, clearer command center.
					</p>
				</div>

				<div className="rounded-2xl border border-[var(--border-color)] bg-[var(--bg-elevated)] p-6 shadow-[var(--shadow-soft)] backdrop-blur-sm sm:p-8">
					<div className="mb-6">
						<h2 className="text-xl font-semibold tracking-tight text-[var(--text-primary)]">
							Sign in
						</h2>
						<p className="mt-1 text-sm text-[var(--text-secondary)]">
							Use the admin credentials configured for this instance.
						</p>
					</div>

					{error && (
						<div className="mb-5 rounded-xl border border-[color:var(--danger)]/25 bg-[color:var(--danger)]/8 px-4 py-3">
							<p className="text-sm text-[color:var(--danger)]">{error}</p>
						</div>
					)}

					<form onSubmit={handleSubmit} className="space-y-5">
						<div>
							<label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
								Email
							</label>
							<div className="relative">
								<Mail className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--text-muted)]" />
								<input
									type="email"
									value={email}
									onChange={(e) => setEmail(e.target.value)}
									placeholder="priest@anubis.watch"
									className="w-full rounded-xl border border-[var(--border-color)] bg-[var(--bg-secondary)] px-11 py-3 text-base text-[var(--text-primary)] placeholder:text-[var(--text-muted)] transition-colors focus:outline-none focus:border-[var(--border-strong)]"
									required
								/>
							</div>
						</div>

						<div>
							<label className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
								Password
							</label>
							<div className="relative">
								<Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--text-muted)]" />
								<input
									type={showPassword ? "text" : "password"}
									value={password}
									onChange={(e) => setPassword(e.target.value)}
									placeholder="••••••••"
									className="w-full rounded-xl border border-[var(--border-color)] bg-[var(--bg-secondary)] px-11 py-3 text-base text-[var(--text-primary)] placeholder:text-[var(--text-muted)] transition-colors focus:outline-none focus:border-[var(--border-strong)]"
									required
								/>
								<button
									type="button"
									onClick={() => setShowPassword(!showPassword)}
									className="absolute right-3 top-1/2 -translate-y-1/2 text-[var(--text-muted)] transition-colors hover:text-[var(--text-primary)]"
									aria-label={showPassword ? "Hide password" : "Show password"}
								>
									{showPassword ? (
										<EyeOff className="h-4 w-4" />
									) : (
										<Eye className="h-4 w-4" />
									)}
								</button>
							</div>
						</div>

						<button
							type="submit"
							disabled={loading}
							className="flex w-full items-center justify-center gap-2 rounded-xl bg-[var(--accent-primary)] px-4 py-3 text-sm font-semibold text-[#0a0a15] transition-all hover:brightness-105 disabled:cursor-not-allowed disabled:opacity-60"
						>
							{loading ? (
								<div className="h-4 w-4 rounded-full border-2 border-[#0a0a15]/25 border-t-[#0a0a15] animate-spin" />
							) : (
								<>
									Enter the Temple
									<ArrowRight className="h-4 w-4" />
								</>
							)}
						</button>
					</form>
				</div>
			</div>
		</div>
	);
}
