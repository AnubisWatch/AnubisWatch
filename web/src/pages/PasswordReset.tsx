import { ArrowLeft, Lock, Mail } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../api/client";

const inputClass =
	"w-full rounded-xl border border-[var(--border-color)] bg-[var(--bg-secondary)] px-11 py-3 text-base text-[var(--text-primary)] placeholder:text-[var(--text-muted)] transition-colors focus:outline-none focus:border-[var(--border-strong)]";
const buttonClass =
	"flex w-full items-center justify-center gap-2 rounded-xl bg-[var(--accent-primary)] px-4 py-3 text-sm font-semibold text-[#0a0a15] transition-all hover:brightness-105 disabled:cursor-not-allowed disabled:opacity-60";

function Card({ title, subtitle, children }: { title: string; subtitle: string; children: React.ReactNode }) {
	return (
		<div className="flex min-h-screen items-center justify-center bg-[var(--bg-primary)] px-4 py-10 text-[var(--text-primary)]">
			<div className="w-full max-w-md rounded-2xl border border-[var(--border-color)] bg-[var(--bg-elevated)] p-6 shadow-[var(--shadow-soft)] sm:p-8">
				<h1 className="text-xl font-semibold tracking-tight">{title}</h1>
				<p className="mt-1 mb-6 text-sm text-[var(--text-secondary)]">{subtitle}</p>
				{children}
				<Link
					to="/login"
					className="mt-6 inline-flex items-center gap-1 text-sm text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
				>
					<ArrowLeft className="h-4 w-4" /> Back to sign in
				</Link>
			</div>
		</div>
	);
}

function errorMessage(err: unknown, fallback: string) {
	return err instanceof Error && err.message ? err.message : fallback;
}

/** Asks the server to email a reset link (only offered when mail is configured). */
export function ForgotPassword() {
	const [email, setEmail] = useState("");
	const [loading, setLoading] = useState(false);
	const [sent, setSent] = useState(false);
	const [error, setError] = useState("");

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		setLoading(true);
		setError("");
		try {
			await api.post("/auth/reset-password", { email });
			setSent(true);
		} catch (err) {
			setError(errorMessage(err, "The request could not be sent. Try again later."));
		} finally {
			setLoading(false);
		}
	};

	return (
		<Card title="Forgot password" subtitle="We will email a link to choose a new password.">
			{sent ? (
				<p role="status" className="text-sm text-[var(--text-secondary)]">
					If that address belongs to an account, a reset link is on its way. It expires in one hour.
				</p>
			) : (
				<form onSubmit={handleSubmit} className="space-y-5">
					{error && <p className="text-sm text-[color:var(--danger)]">{error}</p>}
					<div>
						<label htmlFor="forgot-email" className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
							Email
						</label>
						<div className="relative">
							<Mail className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--text-muted)]" />
							<input
								id="forgot-email"
								type="email"
								value={email}
								onChange={(e) => setEmail(e.target.value)}
								className={inputClass}
								required
							/>
						</div>
					</div>
					<button type="submit" disabled={loading} className={buttonClass}>
						Send reset link
					</button>
				</form>
			)}
		</Card>
	);
}

/** Completes a reset from the emailed /reset-password?token=... link. */
export function ResetPassword() {
	const navigate = useNavigate();
	const [searchParams] = useSearchParams();
	// Keep the token in memory and drop it from the address bar, so it does
	// not linger in history or leak through a Referer.
	const [token] = useState(() => searchParams.get("token") ?? "");
	const [password, setPassword] = useState("");
	const [confirm, setConfirm] = useState("");
	const [loading, setLoading] = useState(false);
	const [done, setDone] = useState(false);
	const [error, setError] = useState("");

	useEffect(() => {
		if (searchParams.has("token")) navigate("/reset-password", { replace: true });
	}, [navigate, searchParams]);

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		setError("");
		if (password !== confirm) {
			setError("The passwords do not match.");
			return;
		}
		setLoading(true);
		try {
			await api.post("/auth/reset-password/confirm", { token, new_password: password });
			setDone(true);
		} catch (err) {
			setError(errorMessage(err, "The password could not be reset."));
		} finally {
			setLoading(false);
		}
	};

	if (!token) {
		return (
			<Card title="Reset password" subtitle="This page needs the link from your reset email.">
				<p className="text-sm text-[var(--text-secondary)]">
					The link is missing or incomplete. Request a new one from the sign-in page.
				</p>
			</Card>
		);
	}

	return (
		<Card title="Reset password" subtitle="At least 12 characters, with three of: uppercase, lowercase, digits, symbols.">
			{done ? (
				<p role="status" className="text-sm text-[var(--text-secondary)]">
					Your password has been reset. Sign in with the new password.
				</p>
			) : (
				<form onSubmit={handleSubmit} className="space-y-5">
					{error && <p className="text-sm text-[color:var(--danger)]">{error}</p>}
					{[
						{ id: "reset-password", label: "New password", value: password, set: setPassword },
						{ id: "reset-confirm", label: "Confirm new password", value: confirm, set: setConfirm },
					].map((field) => (
						<div key={field.id}>
							<label htmlFor={field.id} className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
								{field.label}
							</label>
							<div className="relative">
								<Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--text-muted)]" />
								<input
									id={field.id}
									type="password"
									autoComplete="new-password"
									minLength={12}
									value={field.value}
									onChange={(e) => field.set(e.target.value)}
									className={inputClass}
									required
								/>
							</div>
						</div>
					))}
					<button type="submit" disabled={loading} className={buttonClass}>
						Set new password
					</button>
				</form>
			)}
		</Card>
	);
}

/**
 * /accept-invite?token=…: an invitee sets their name and password. The token
 * is single-use; the account exists once this succeeds and signs in normally.
 */
export function AcceptInvite() {
	const navigate = useNavigate();
	const [searchParams] = useSearchParams();
	// Like the reset token: keep it in memory, drop it from the address bar.
	const [token] = useState(() => searchParams.get("token") ?? "");
	const [name, setName] = useState("");
	const [password, setPassword] = useState("");
	const [confirm, setConfirm] = useState("");
	const [loading, setLoading] = useState(false);
	const [done, setDone] = useState(false);
	const [error, setError] = useState("");

	useEffect(() => {
		if (searchParams.has("token")) navigate("/accept-invite", { replace: true });
	}, [navigate, searchParams]);

	const handleSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		setError("");
		if (password !== confirm) {
			setError("The passwords do not match.");
			return;
		}
		setLoading(true);
		try {
			await api.post("/auth/accept-invite", { token, name: name.trim(), password });
			setDone(true);
		} catch (err) {
			setError(errorMessage(err, "The invitation could not be accepted."));
		} finally {
			setLoading(false);
		}
	};

	if (!token) {
		return (
			<Card title="Accept invitation" subtitle="This page needs the link from your invitation.">
				<p className="text-sm text-[var(--text-secondary)]">
					The link is missing or incomplete. Ask the person who invited you for a new one.
				</p>
			</Card>
		);
	}

	return (
		<Card
			title="Accept invitation"
			subtitle="New here? Choose your name and a password (at least 12 characters, three of uppercase, lowercase, digits, symbols). Already have an account? Leave the name empty and enter your current password."
		>
			{done ? (
				<p role="status" className="text-sm text-[var(--text-secondary)]">
					Invitation accepted. Sign in with your email and password.
				</p>
			) : (
				<form onSubmit={handleSubmit} className="space-y-5">
					{error && <p className="text-sm text-[color:var(--danger)]">{error}</p>}
					<div>
						<label htmlFor="invite-name" className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
							Name
						</label>
						<input
							id="invite-name"
							autoComplete="name"
							maxLength={100}
							value={name}
							onChange={(e) => setName(e.target.value)}
							className={inputClass.replace("px-11", "px-4")}
						/>
					</div>
					{[
						{ id: "invite-password", label: "Password", value: password, set: setPassword },
						{ id: "invite-confirm", label: "Confirm password", value: confirm, set: setConfirm },
					].map((field) => (
						<div key={field.id}>
							<label htmlFor={field.id} className="mb-2 block text-sm font-medium text-[var(--text-secondary)]">
								{field.label}
							</label>
							<div className="relative">
								<Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--text-muted)]" />
								<input
									id={field.id}
									type="password"
									autoComplete="new-password"
									minLength={12}
									value={field.value}
									onChange={(e) => field.set(e.target.value)}
									className={inputClass}
									required
								/>
							</div>
						</div>
					))}
					<button type="submit" disabled={loading} className={buttonClass}>
						Create account
					</button>
				</form>
			)}
		</Card>
	);
}
