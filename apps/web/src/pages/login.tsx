import { Brand } from "@/components/ui/Brand";
import { AuthForm } from "@/components/auth/AuthForm";
export default function Login() {
  return (
    <main className="auth-page">
      <section className="auth-visual">
        <Brand light />
        <div className="auth-story">
          <span className="eyebrow">LESS NOISE. MORE POSSIBILITY.</span>
          <h2>
            Your work,
            <br />
            in good shape.
          </h2>
          <p>
            A thoughtful home for your projects.
            <br />
            And the space to make them happen.
          </p>
        </div>
        <div className="login-art" aria-hidden="true">
          <div />
          <div />
          <div />
          <span>✳</span>
        </div>
        <p className="auth-footer">A little clarity. A lot of possibility.</p>
      </section>
      <section className="auth-main">
        <div className="mobile-brand">
          <Brand />
        </div>
        <AuthForm />
        <span className="login-bottom">
          FORMA WORKSPACE <span>DESIGNED TO MOVE YOU FORWARD</span>
        </span>
      </section>
    </main>
  );
}

// Request rendering supplies a fresh CSP nonce and per-request Styletron styles.
export const getServerSideProps = async () => ({ props: {} });
