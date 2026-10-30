import { useState } from "react";
import { useRouter } from "next/router";
import { ArrowRight, ArrowUpRight, LockKeyhole } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { Field } from "@/components/ui/Field";
import { ErrorNotice } from "@/components/ui/Feedback";
import { request, errorMessage } from "@/lib/api";
import { apiRoutes, routes } from "@/lib/routes";
export function AuthForm() {
  const [register, setRegister] = useState(false);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      await request(register ? apiRoutes.register : apiRoutes.login, {
        method: "POST",
        body: JSON.stringify({ name, email, password }),
      });
      await router.push(routes.overview);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="auth-form">
      <span className="eyebrow">A LITTLE SPACE TO DO GREAT THINGS</span>
      <h1>{register ? "Your next chapter." : "Right where you left off."}</h1>
      <p className="description">
        {register
          ? "Create your account. Make room for your ideas."
          : "Welcome back. Let’s make something good."}
      </p>
      {error && <ErrorNotice message={error} />}
      <form onSubmit={submit}>
        {register && (
          <Field
            label="Your name"
            autoComplete="name"
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
            required
            maxLength={80}
            disabled={busy}
          />
        )}
        <Field
          label="Email address"
          type="email"
          autoComplete="email"
          placeholder="you@company.com"
          value={email}
          onChange={(e) => setEmail(e.currentTarget.value)}
          required
          maxLength={254}
          disabled={busy}
        />
        <Field
          label="Password"
          type="password"
          autoComplete={register ? "new-password" : "current-password"}
          value={password}
          onChange={(e) => setPassword(e.currentTarget.value)}
          required
          minLength={12}
          maxLength={128}
          disabled={busy}
          caption={
            register
              ? "Use at least 12 characters. A passphrase works well."
              : undefined
          }
        />
        <Button
          type="submit"
          isLoading={busy}
          endEnhancer={() => <ArrowRight size={16} />}
          overrides={{
            BaseButton: {
              style: {
                width: "100%",
                marginTop: "8px",
                minHeight: "43px",
                fontSize: "14px",
                fontWeight: 600,
              },
            },
          }}
        >
          {register ? "Create your workspace" : "Sign in to your workspace"}
        </Button>
      </form>
      <p className="auth-switch">
        {register ? "Already have an account?" : "New here?"}{" "}
        <button
          disabled={busy}
          onClick={() => {
            setRegister(!register);
            setError("");
          }}
        >
          {register ? "Sign in" : "Create an account"}{" "}
          <ArrowUpRight size={13} />
        </button>
      </p>
      <div className="auth-security">
        <LockKeyhole size={14} />
        Your ideas stay yours. A private, secure workspace.
      </div>
    </div>
  );
}
