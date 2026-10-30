import { useState, type ReactNode } from "react";
import { useRouter } from "next/router";
import { ChevronRight, ShieldCheck, X, LogOut } from "lucide-react";
import { Modal, ModalHeader, ModalBody, ModalFooter } from "baseui/modal";
import { Sidebar } from "./Sidebar";
import { Button } from "@/components/ui/Button";
import { ErrorNotice } from "@/components/ui/Feedback";
import { request, errorMessage } from "@/lib/api";
import { apiRoutes, routes } from "@/lib/routes";
import type { Session } from "@/lib/types";
export function AppShell({
  session,
  title,
  children,
}: {
  session: Session;
  title: string;
  children: ReactNode;
}) {
  const router = useRouter();
  const [loggingOut, setLoggingOut] = useState(false);
  const [error, setError] = useState("");
  const [help, setHelp] = useState(false);
  async function logout() {
    setLoggingOut(true);
    setError("");
    try {
      await request(
        apiRoutes.logout,
        { method: "POST", body: "{}" },
        session.csrfToken,
      );
      await router.push(routes.login);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoggingOut(false);
    }
  }
  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <Sidebar
        user={session.user}
        onLogout={logout}
        loggingOut={loggingOut}
        onHelp={() => setHelp(true)}
      />
      <div className="main-frame">
        <div className="topbar">
          <div className="breadcrumbs">
            <span>Workspace</span>
            <ChevronRight size={13} />
            <strong>{title}</strong>
          </div>
          <span className="private-label">
            <ShieldCheck size={14} />
            Private workspace
          </span>
          <button
            className="mobile-logout icon-button"
            aria-label="Sign out"
            onClick={logout}
            disabled={loggingOut}
          >
            <LogOut size={17} />
          </button>
        </div>
        <main id="main" className="main-content">
          {error && <ErrorNotice message={error} />}
          {children}
        </main>
        <footer className="app-footer">
          <span>A little clarity. A lot of possibility.</span>
          <span>Made for the work that matters.</span>
        </footer>
      </div>
      <Modal isOpen={help} onClose={() => setHelp(false)} closeable>
        <ModalHeader>Make yourself at home.</ModalHeader>
        <ModalBody>
          <p>Forma keeps your projects together in a private workspace.</p>
          <ol className="help-steps">
            <li>Create a project with a name and a short description.</li>
            <li>Choose a status: planned, in progress, or completed.</li>
            <li>
              Open any project to update its details as your work takes shape.
            </li>
          </ol>
          <p>
            Your overview stays in sync with your projects. Manage your sign-in
            sessions in Account & security.
          </p>
        </ModalBody>
        <ModalFooter>
          <Button onClick={() => setHelp(false)}>
            <X size={15} />
            Got it
          </Button>
        </ModalFooter>
      </Modal>
    </div>
  );
}
