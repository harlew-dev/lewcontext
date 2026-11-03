import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MfaScanStep, RecoveryCodeList } from "./mfa-setup";
import { qrPath } from "./qr-code";
const setup = {
  secret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
  uri: "otpauth://totp/lewcontext:ada%40example.com?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
};
const codes = ["k7p2m-x9qfr", "a1b2c-d3e4f"];
describe("MfaScanStep", () => {
  it("keeps the QR code and the key off the page until asked for", async () => {
    const { container } = render(
      <MfaScanStep
        setup={setup}
        code=""
        onCodeChange={() => {}}
        disabled={false}
      />,
    );
    const real = qrPath(setup.uri).path;
    expect(container.innerHTML).not.toContain(setup.secret);
    expect(container.innerHTML).not.toContain(real);
    expect(screen.queryByRole("img", { name: /QR code/ })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Reveal QR code" }));
    const qr = screen.getByRole("img", {
      name: "QR code to add this account to your authenticator app",
    });
    expect(qr.querySelector("path")).toHaveAttribute("d", real);
    expect(container.innerHTML).not.toContain(setup.secret);

    fireEvent.click(
      screen.getByRole("button", { name: "Use setup key instead" }),
    );
    // The field starts masked: the key isn't in the page until shown.
    const key = screen.getByLabelText("Setup key");
    expect(key).toHaveValue("•••• •••• •••• •••• •••• •••• •••• ••••");
    expect(key).toHaveAttribute("readonly");
    expect(container.innerHTML).not.toContain(setup.secret);
    fireEvent.click(screen.getByRole("button", { name: "Show the key" }));
    expect(key).toHaveValue("JBSW Y3DP EHPK 3PXP JBSW Y3DP EHPK 3PXP");
    fireEvent.click(screen.getByRole("button", { name: "Mask the key" }));
    expect(container.innerHTML).not.toContain("JBSW Y3DP");

    // Copy works masked and takes the key without spaces; Hide setup key
    // puts the link back, focused.
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    fireEvent.click(screen.getByRole("button", { name: "Copy setup key" }));
    expect(writeText).toHaveBeenCalledWith(setup.secret);
    fireEvent.click(screen.getByRole("button", { name: "Hide setup key" }));
    expect(screen.queryByLabelText("Setup key")).toBeNull();
    await waitFor(() =>
      expect(
        screen.getByRole("button", {
          name: "Use setup key instead",
        }),
      ).toHaveFocus(),
    );
  });
  it("takes the code from the app", () => {
    const onCodeChange = vi.fn();
    render(
      <MfaScanStep
        setup={setup}
        code=""
        onCodeChange={onCodeChange}
        error="That code didn’t match."
        disabled={false}
      />,
    );
    const field = screen.getByLabelText("Code from your app");
    expect(field).toHaveAttribute("inputmode", "numeric");
    expect(field).toHaveAttribute("autocomplete", "one-time-code");
    fireEvent.change(field, { target: { value: "123456" } });
    expect(onCodeChange).toHaveBeenCalledWith("123456");
    expect(screen.getByRole("alert")).toHaveTextContent(
      "That code didn’t match.",
    );
  });
});
describe("RecoveryCodeList", () => {
  it("shows the codes only when revealed, which counts as saved", () => {
    const onSaved = vi.fn();
    render(
      <RecoveryCodeList
        codes={codes}
        account="ada@example.com"
        onSaved={onSaved}
      />,
    );
    expect(document.body).not.toHaveTextContent("k7p2m-x9qfr");
    expect(onSaved).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Reveal recovery codes" }),
    );
    expect(
      screen.getByRole("list", { name: "Recovery codes" }),
    ).toHaveTextContent("k7p2m-x9qfra1b2c-d3e4f");
    expect(onSaved).toHaveBeenCalledTimes(1);
  });
  it("copies them without revealing them", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const onSaved = vi.fn();
    render(
      <RecoveryCodeList
        codes={codes}
        account="ada@example.com"
        onSaved={onSaved}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Copy codes" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(writeText).toHaveBeenCalledWith("k7p2m-x9qfr\na1b2c-d3e4f");
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Copied");
    expect(document.body).not.toHaveTextContent("k7p2m-x9qfr");
  });
  it("downloads them as a text file", async () => {
    let saved: Blob | undefined;
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL(blob: Blob) {
          saved = blob;
          return "blob:codes";
        }
        static revokeObjectURL() {}
      },
    );
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});
    const onSaved = vi.fn();
    render(
      <RecoveryCodeList
        codes={codes}
        account="ada@example.com"
        onSaved={onSaved}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    expect(click).toHaveBeenCalled();
    expect(onSaved).toHaveBeenCalled();
    const text = await new Promise<string>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.readAsText(saved!);
    });
    expect(text).toContain("ada@example.com");
    expect(text).toContain("k7p2m-x9qfr\na1b2c-d3e4f");
    click.mockRestore();
  });
});
