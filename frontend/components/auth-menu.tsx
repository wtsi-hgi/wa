"use client";

import {
    type FormEvent,
    type ReactNode,
    useEffect,
    useRef,
    useState,
} from "react";
import { LogIn, LogOut, LockKeyhole, User } from "lucide-react";
import { useRouter } from "next/navigation";

import { Alert } from "@/components/ui/alert";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from "@/components/ui/tooltip";
import { loginAction, type CurrentSession } from "@/app/(results)/auth/actions";

type AuthMenuProps = {
    initialSession: CurrentSession;
};

function AccountAvatar({ username }: { username: string | null }): ReactNode {
    const trimmedUsername = username?.trim() ?? "";
    const initial = trimmedUsername.charAt(0).toUpperCase();

    return (
        <Avatar className="border border-border bg-primary text-primary-foreground shadow-sm">
            <AvatarFallback className="bg-primary text-sm font-semibold text-primary-foreground">
                {initial ? (
                    <span aria-hidden="true">{initial}</span>
                ) : (
                    <User aria-hidden="true" className="h-4 w-4" />
                )}
            </AvatarFallback>
        </Avatar>
    );
}

function anonymousSession(): CurrentSession {
    return {
        authenticated: false,
        username: null,
    };
}

async function logoutFromBrowser(): Promise<CurrentSession> {
    const response = await fetch("/api/auth/logout", {
        cache: "no-store",
        credentials: "same-origin",
        method: "POST",
    });

    if (!response.ok) {
        return anonymousSession();
    }

    const body = (await response
        .json()
        .catch(() => null)) as CurrentSession | null;

    return body?.authenticated ? body : anonymousSession();
}

async function refreshFromBrowser(): Promise<CurrentSession> {
    const response = await fetch("/api/auth/refresh", {
        cache: "no-store",
        credentials: "same-origin",
        method: "POST",
    });

    if (!response.ok) {
        return anonymousSession();
    }

    const body = (await response
        .json()
        .catch(() => null)) as CurrentSession | null;

    return body?.authenticated ? body : anonymousSession();
}

export function AuthMenu({ initialSession }: AuthMenuProps): ReactNode {
    const router = useRouter();
    const refreshRoute = router.refresh;
    const [session, setSession] = useState<CurrentSession>(initialSession);
    const [loginOpen, setLoginOpen] = useState(false);
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [loginError, setLoginError] = useState<string | null>(null);
    const [loginPending, setLoginPending] = useState(false);
    const [logoutPending, setLogoutPending] = useState(false);
    const refreshGenerationRef = useRef(0);
    const usernameInputRef = useRef<HTMLInputElement | null>(null);

    function focusUsernameInput(): void {
        const schedule =
            window.requestAnimationFrame ??
            ((callback: FrameRequestCallback) =>
                window.setTimeout(callback, 0));

        schedule(() => {
            usernameInputRef.current?.focus();
        });
    }

    function openLogin(): void {
        setLoginOpen(true);
        setLoginError(null);
        focusUsernameInput();
    }

    useEffect(() => {
        if (!session.authenticated) {
            return;
        }

        let cancelled = false;
        const refreshGeneration = refreshGenerationRef.current;

        async function refreshSession(): Promise<void> {
            try {
                const nextSession = await refreshFromBrowser();

                if (
                    cancelled ||
                    refreshGeneration !== refreshGenerationRef.current
                ) {
                    return;
                }

                setSession((currentSession) =>
                    currentSession.authenticated ===
                        nextSession.authenticated &&
                    currentSession.username === nextSession.username
                        ? currentSession
                        : nextSession,
                );
                if (!nextSession.authenticated) {
                    refreshRoute();
                }
            } catch {
                // Keep the rendered session if the browser cannot reach Next.
            }
        }

        void refreshSession();

        return () => {
            cancelled = true;
        };
    }, [refreshRoute, session.authenticated]);

    async function handleLoginSubmit(
        event: FormEvent<HTMLFormElement>,
    ): Promise<void> {
        event.preventDefault();

        if (loginPending) {
            return;
        }

        setLoginPending(true);
        setLoginError(null);

        try {
            const nextSession = await loginAction({ password, username });

            setSession(
                nextSession.authenticated ? nextSession : anonymousSession(),
            );
            setPassword("");
            setLoginOpen(false);
            refreshRoute();
        } catch {
            setLoginError("Authentication failed");
            setLoginOpen(true);
            focusUsernameInput();
        } finally {
            setLoginPending(false);
        }
    }

    async function handleLogout(): Promise<void> {
        if (logoutPending) {
            return;
        }

        setLogoutPending(true);
        refreshGenerationRef.current += 1;

        try {
            const nextSession = await logoutFromBrowser();

            setSession(nextSession);
        } catch {
            setSession(anonymousSession());
        } finally {
            setUsername("");
            setPassword("");
            setLoginError(null);
            setLoginOpen(false);
            refreshRoute();
            setLogoutPending(false);
        }
    }

    if (session.authenticated) {
        const accountName = session.username ?? "Signed in";

        return (
            <DropdownMenu>
                <DropdownMenuTrigger
                    aria-label={`${accountName} account`}
                    className="inline-flex h-11 items-center gap-2 rounded-md border border-border bg-background/92 px-1.5 pr-2.5 text-sm font-medium text-foreground shadow-sm transition hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
                >
                    <AccountAvatar username={session.username} />
                    <Badge className="h-7">{accountName}</Badge>
                </DropdownMenuTrigger>
                <DropdownMenuContent className="w-64 rounded-md">
                    <div className="flex items-center gap-3 px-3 py-2">
                        <AccountAvatar username={session.username} />
                        <div className="min-w-0">
                            <div className="truncate text-sm font-semibold text-foreground">
                                {accountName}
                            </div>
                            <Badge className="mt-1 h-6 bg-muted text-muted-foreground">
                                Signed in
                            </Badge>
                        </div>
                    </div>
                    <div className="my-1 h-px bg-border" />
                    <button
                        type="button"
                        role="menuitem"
                        className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm font-medium text-foreground transition hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50"
                        disabled={logoutPending}
                        onClick={() => {
                            void handleLogout();
                        }}
                    >
                        <LogOut aria-hidden="true" className="h-4 w-4" />
                        <span>Log out</span>
                    </button>
                </DropdownMenuContent>
            </DropdownMenu>
        );
    }

    return (
        <div className="flex max-w-full flex-col items-end gap-2">
            <Tooltip>
                <TooltipTrigger asChild>
                    <Button
                        type="button"
                        aria-expanded={loginOpen}
                        aria-haspopup="dialog"
                        onClick={openLogin}
                        variant="outline"
                    >
                        <LogIn aria-hidden="true" className="h-4 w-4" />
                        <span>Log in</span>
                    </Button>
                </TooltipTrigger>
                <TooltipContent>Log in</TooltipContent>
            </Tooltip>

            {loginOpen ? (
                <div className="w-[min(20rem,calc(100vw-2rem))] rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-[0_20px_60px_-28px_rgba(41,58,85,0.55)]">
                    <form
                        aria-label="Log in"
                        aria-describedby={
                            loginError ? "auth-menu-login-error" : undefined
                        }
                        className="flex flex-col gap-3"
                        onSubmit={(event) => {
                            void handleLoginSubmit(event);
                        }}
                    >
                        <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
                            <LockKeyhole
                                aria-hidden="true"
                                className="h-4 w-4 text-muted-foreground"
                            />
                            <span>WA access</span>
                        </div>

                        {loginError ? (
                            <Alert
                                id="auth-menu-login-error"
                                role="alert"
                                aria-live="assertive"
                                className="flex items-center gap-2 border-destructive/35 bg-destructive/10 font-medium text-destructive"
                            >
                                <LockKeyhole
                                    aria-hidden="true"
                                    className="h-4 w-4 shrink-0"
                                />
                                {loginError}
                            </Alert>
                        ) : null}

                        <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">
                            <span>Username</span>
                            <input
                                ref={usernameInputRef}
                                autoComplete="username"
                                className="h-10 rounded-md border border-input bg-background px-3 text-sm text-foreground shadow-inner outline-none transition focus-visible:ring-2 focus-visible:ring-ring"
                                name="username"
                                value={username}
                                onChange={(event) => {
                                    setUsername(event.target.value);
                                }}
                            />
                        </label>

                        <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">
                            <span>Password</span>
                            <input
                                autoComplete="current-password"
                                className="h-10 rounded-md border border-input bg-background px-3 text-sm text-foreground shadow-inner outline-none transition focus-visible:ring-2 focus-visible:ring-ring"
                                name="password"
                                type="password"
                                value={password}
                                onChange={(event) => {
                                    setPassword(event.target.value);
                                }}
                            />
                        </label>

                        <div className="flex items-center justify-end gap-2 pt-1">
                            <Button
                                type="button"
                                disabled={loginPending}
                                onClick={() => {
                                    setLoginOpen(false);
                                    setLoginError(null);
                                }}
                                size="sm"
                                variant="ghost"
                            >
                                Cancel
                            </Button>
                            <Button
                                disabled={loginPending}
                                size="sm"
                                type="submit"
                            >
                                <LogIn aria-hidden="true" className="h-4 w-4" />
                                <span>Continue</span>
                            </Button>
                        </div>
                    </form>
                </div>
            ) : null}
        </div>
    );
}
