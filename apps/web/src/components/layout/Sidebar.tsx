import Link from "next/link";
import { useRouter } from "next/router";
import { ChevronsUpDown, ArrowUpRight, LogOut, CircleHelp } from "lucide-react";
import { Brand } from "@/components/ui/Brand";
import { navigation } from "@/lib/routes";
import type { User } from "@/lib/types";
export function Sidebar({
  user,
  onLogout,
  loggingOut,
  onHelp,
}: {
  user: User;
  onLogout: () => void;
  loggingOut: boolean;
  onHelp: () => void;
}) {
  const router = useRouter();
  const initials = user.name
    .split(" ")
    .map((part) => part[0])
    .slice(0, 2)
    .join("");
  return (
    <aside className="sidebar">
      <Link href="/" className="brand-link" aria-label="Forma home">
        <Brand />
      </Link>
      <div className="workspace-switcher">
        <span className="workspace-icon">F</span>
        <div>
          <strong>Personal workspace</strong>
          <small>A space for your best work</small>
        </div>
        <ChevronsUpDown size={14} />
      </div>
      <p className="nav-label">WORKSPACE</p>
      <nav aria-label="Main navigation">
        {navigation.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            aria-label={label}
            className={`nav-link ${router.pathname === href ? "active" : ""}`}
            aria-current={router.pathname === href ? "page" : undefined}
          >
            <Icon size={18} strokeWidth={1.65} />
            <span>{label}</span>
            {router.pathname === href && <span className="nav-indicator" />}
          </Link>
        ))}
      </nav>
      <div className="sidebar-bottom">
        <div className="sidebar-note">
          <span className="note-symbol">✳</span>
          <h3>Good things take shape.</h3>
          <p>
            A little structure.
            <br />A lot of possibility.
          </p>
          <Link href="/projects">
            Make room for your ideas <ArrowUpRight size={14} />
          </Link>
        </div>
        <button className="help-link" onClick={onHelp}>
          <CircleHelp size={17} />
          Getting started
        </button>
        <div className="profile">
          <span className="avatar">{initials}</span>
          <div className="profile-name">
            <strong>{user.name}</strong>
            <small>{user.email}</small>
          </div>
          <button
            className="icon-button"
            onClick={onLogout}
            disabled={loggingOut}
            aria-label="Sign out"
            title="Sign out"
          >
            <LogOut size={17} />
          </button>
        </div>
      </div>
    </aside>
  );
}
