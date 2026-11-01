import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { ChallengeDialog } from "@/components/app/challenge/challenge-dialog";
import { ApiError, type Challenge, type Solve } from "@/lib/api/http";
// Challenges, from inside the HTTP client: when a rate limit's challenge
// level answers 428, request() calls solve (useApi passes it), which opens
// ChallengeDialog and resolves with the CAPTCHA's token, so no page or form
// handles it. Mounted once in root.tsx. Challenges queue: two at once show
// one after the other, since each token works once.

const SolveContext = createContext<Solve | undefined>(undefined);

// The page's solve, or undefined outside a ChallengeProvider (tests).
export function useChallenge(): Solve | undefined {
  return useContext(SolveContext);
}

type Pending = {
  id: number;
  challenge: Challenge;
  resolve: (token: string) => void;
  reject: (error: Error) => void;
};

export function ChallengeProvider({ children }: { children: ReactNode }) {
  const [queue, setQueue] = useState<Pending[]>([]);
  const next = useRef(0);
  const solve = useCallback<Solve>(
    (challenge) =>
      new Promise<string>((resolve, reject) => {
        next.current += 1;
        const id = next.current;
        setQueue((q) => [...q, { id, challenge, resolve, reject }]);
      }),
    [],
  );
  const current = queue[0];
  const finish = useMemo(
    () => ({
      solved(token: string) {
        current?.resolve(token);
        setQueue((q) => q.slice(1));
      },
      cancelled() {
        current?.reject(new ApiError("Check cancelled.", 428));
        setQueue((q) => q.slice(1));
      },
    }),
    [current],
  );
  return (
    <SolveContext.Provider value={solve}>
      {children}
      <ChallengeDialog
        key={current?.id ?? 0}
        challenge={current?.challenge ?? null}
        onSolved={finish.solved}
        onCancel={finish.cancelled}
      />
    </SolveContext.Provider>
  );
}
