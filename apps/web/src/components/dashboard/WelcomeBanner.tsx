import { ArrowRight } from "lucide-react";

export function WelcomeBanner({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="welcome-banner">
      <div>
        <span className="banner-tag">MAKE SPACE FOR WHAT’S NEXT</span>
        <h2>
          Big ideas.
          <br />
          Small, steady steps.
        </h2>
        <p>
          Bring your projects together.
          <br />
          Give your best work room to grow.
        </p>
        <button onClick={onCreate}>
          Start something good <ArrowRight size={16} />
        </button>
      </div>
      <div className="banner-art" aria-hidden="true">
        <div className="art-orbit orbit-one" />
        <div className="art-orbit orbit-two" />
        <div className="art-block block-one" />
        <div className="art-block block-two" />
        <div className="art-block block-three" />
        <span className="art-spark">✳</span>
        <span className="art-dot" />
        <span className="art-caption">GOOD THINGS ARE TAKING SHAPE.</span>
      </div>
    </div>
  );
}
