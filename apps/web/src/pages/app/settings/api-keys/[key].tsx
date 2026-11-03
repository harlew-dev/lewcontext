import { useParams } from "react-router";
import { KeyPage } from "@/components/app/api-keys/key-page";
import { parsePublicId } from "@/lib/ids";
// One of your own personal API keys: Overview and Logs (its own events only).
export default function Show() {
  const id = parsePublicId("apk", useParams().key);
  return <KeyPage key={id} id={id ?? "missing"} />;
}
