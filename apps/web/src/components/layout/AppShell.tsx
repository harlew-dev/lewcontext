import { useState, type ReactNode } from "react";
import { useRouter } from "next/router";
import { ChevronRight, ShieldCheck, LogOut, X } from "lucide-react";
import { Modal, ModalHeader, ModalBody, ModalFooter } from "baseui/modal";
import { Sidebar } from "./Sidebar";
import { Button } from "@/components/ui/Button";
import { IconButton } from "@/components/ui/IconButton";
import { Notice } from "@/components/ui/Feedback";
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
    <div className="min-h-screen py-2.5 pr-2.5 pl-[242px] max-lg:pl-[210px] max-md:px-3 max-md:pt-0 max-md:pb-3">
      <a
        className="fixed -top-15 left-2.5 z-[10000] bg-white p-3.75 focus:top-2.5"
        href="#main"
      >
        Skip to content
      </a>
      <Sidebar
        user={session.user}
        onLogout={logout}
        loggingOut={loggingOut}
        onHelp={() => setHelp(true)}
      />
      <div className="flex min-h-[calc(100vh-20px)] flex-col rounded-xl border border-edge bg-white shadow-frame max-md:min-h-[calc(100vh-100px)]">
        <div className="flex h-15 items-center justify-between border-b border-mauve-100 px-9.5 max-lg:px-7 max-md:h-12 max-md:px-5.75 max-sm:h-10.75 max-sm:px-4.5 xl:px-13.75">
          <div className="flex items-center gap-2.75 text-label text-haze-400">
            <span>Workspace</span>
            <ChevronRight size={13} className="text-haze-300" />
            <strong className="font-medium text-haze-700">{title}</strong>
          </div>
          <span className="flex items-center gap-1.5 text-caption text-haze-400 max-md:hidden">
            <ShieldCheck size={14} />
            Private workspace
          </span>
          <IconButton
            className="md:hidden"
            aria-label="Sign out"
            onClick={logout}
            disabled={loggingOut}
          >
            <LogOut size={17} />
          </IconButton>
        </div>
        <main
          id="main"
          className="mx-auto w-full max-w-[1480px] flex-1 px-9.5 pt-9.25 pb-9 max-lg:p-7 max-md:px-5.75 max-md:py-6.75 max-sm:px-4.5 max-sm:py-6 xl:px-13.75 xl:py-11.25"
        >
          {error && <Notice variant="error">{error}</Notice>}
          {children}
        </main>
        <footer className="flex items-center justify-between border-t border-mauve-100 px-9.5 py-4.5 text-tiny text-[#aaa3af] max-sm:px-4.5 max-sm:py-4.25 max-sm:text-micro xl:px-13.75">
          <span>A little clarity. A lot of possibility.</span>
          <span className="max-sm:hidden">Made for the work that matters.</span>
        </footer>
      </div>
      <HelpDialog isOpen={help} onClose={() => setHelp(false)} />
    </div>
  );
}
function HelpDialog({
  isOpen,
  onClose,
}: {
  isOpen: boolean;
  onClose: () => void;
}) {
  return (
    <Modal isOpen={isOpen} onClose={onClose} closeable>
      <ModalHeader>Make yourself at home.</ModalHeader>
      <ModalBody>
        <p>Forma keeps your projects together in a private workspace.</p>
        <ol className="my-4 list-decimal pl-5 leading-[1.8] [&>li]:py-2.25">
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
        <Button onClick={onClose}>
          <X size={15} />
          Got it
        </Button>
      </ModalFooter>
    </Modal>
  );
}
