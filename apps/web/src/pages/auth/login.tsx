import { useState } from "react";
import { useRouter } from "next/router";
import { ArrowRight, ArrowUpRight, LockKeyhole } from "lucide-react";
import { Brand } from "@/components/ui/Brand";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Form";
import { Notice } from "@/components/ui/Feedback";
import { Description, Eyebrow } from "@/components/ui/Typography";
import { request, errorMessage } from "@/lib/api";
import { apiRoutes, routes } from "@/lib/routes";
// Request rendering supplies a fresh CSP nonce and per-request Styletron styles.
export const getServerSideProps = async () => ({ props: {} });
export default function Login() {
  return (
    <main className="grid min-h-screen grid-cols-[46%_54%] max-md:grid-cols-[40%_60%] max-sm:block">
      <AuthShowcase />
      <section className="relative flex items-center justify-center bg-[#fdfcfe] p-15 max-lg:p-10 max-md:p-7.5 max-sm:min-h-screen max-sm:px-7 max-sm:py-25">
        <div className="absolute top-7.5 left-7 hidden max-sm:block">
          <Brand />
        </div>
        <AuthForm />
        <span className="absolute right-11.25 bottom-8.75 left-11.25 flex justify-between text-fine tracking-[1px] text-mauve-400 max-md:right-7.5 max-md:left-7.5 max-sm:bottom-6.25">
          FORMA WORKSPACE{" "}
          <span className="max-md:hidden">DESIGNED TO MOVE YOU FORWARD</span>
        </span>
      </section>
    </main>
  );
}
function AuthShowcase() {
  return (
    <section className="relative min-h-[760px] overflow-hidden bg-mauve-950 px-13.75 py-11.5 text-mauve-text max-lg:p-10 max-md:px-6.25 max-md:py-8 max-sm:hidden">
      <Brand light />
      <div className="relative z-2 mt-22.5">
        <Eyebrow variant="inverse" className="mb-2.75">
          LESS NOISE. MORE POSSIBILITY.
        </Eyebrow>
        <h2 className="mt-4.5 text-[55px] leading-[1.1] font-[450] tracking-[-2px] max-lg:text-[46px] max-md:text-[36px]">
          Your work,
          <br />
          in good shape.
        </h2>
        <p className="mt-5.5 text-copy leading-[1.8] text-[#b3a6be] max-md:text-small">
          A thoughtful home for your projects.
          <br />
          And the space to make them happen.
        </p>
      </div>
      <LoginArt />
      <p className="absolute bottom-9 text-caption tracking-[0.2px] text-[#9e90ab]">
        A little clarity. A lot of possibility.
      </p>
    </section>
  );
}
// Decorative shapes on the dark sign-in panel.
function LoginArt() {
  return (
    <div
      className="absolute bottom-20 left-1/2 h-[270px] w-[350px] -translate-x-1/2 max-md:scale-75"
      aria-hidden="true"
    >
      <div className="absolute top-12.5 left-7.5 size-[155px] -rotate-18 rounded-[50%_50%_5%_5%] bg-[linear-gradient(100deg,#9383aa,#baa6c7)] shadow-art-dark" />
      <div className="absolute top-22.5 left-[173px] size-[135px] rotate-15 rounded-lg bg-[linear-gradient(110deg,#bcae9d,#deceb7)] shadow-art-dark" />
      <div className="absolute top-0 left-[173px] size-22.5 rounded-full bg-[linear-gradient(90deg,#777285,#a8a1b8)] shadow-art-dark" />
      <span className="absolute top-3 -right-2.5 text-[47px] text-[#b3a0c6]">
        ✳
      </span>
    </div>
  );
}
function AuthForm() {
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
      await router.push(routes.dashboard);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="w-full max-w-[365px]">
      <Eyebrow variant="form" className="mb-4.25">
        A LITTLE SPACE TO DO GREAT THINGS
      </Eyebrow>
      <h1 className="text-[29px] leading-[1.25] tracking-[-1px] max-sm:text-[28px]">
        {register ? "Your next chapter." : "Right where you left off."}
      </h1>
      <Description className="mt-3.25 mb-8">
        {register
          ? "Create your account. Make room for your ideas."
          : "Welcome back. Let’s make something good."}
      </Description>
      {error && <Notice variant="error">{error}</Notice>}
      <form onSubmit={submit}>
        {register && (
          <Input
            label="Your name"
            autoComplete="name"
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
            required
            maxLength={80}
            disabled={busy}
          />
        )}
        <Input
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
        <Input
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
      <p className="mt-6 text-center text-small text-mauve-600">
        {register ? "Already have an account?" : "New here?"}{" "}
        <button
          type="button"
          className="inline-flex items-center gap-1 font-semibold text-mauve-800"
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
      <div className="mt-8 flex items-center justify-center gap-1.75 border-t border-mauve-200 pt-6 text-caption text-mauve-500 max-sm:text-tiny">
        <LockKeyhole size={14} />
        Your ideas stay yours. A private, secure workspace.
      </div>
    </div>
  );
}
