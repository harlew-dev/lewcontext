import Link from "next/link";
export default function NotFound() {
  return (
    <main className="service-unavailable">
      <span className="eyebrow">A SMALL DETOUR</span>
      <h1>This page has wandered off.</h1>
      <p>Let’s get you back to your workspace.</p>
      <Link className="text-link" href="/">
        Back to overview →
      </Link>
    </main>
  );
}
