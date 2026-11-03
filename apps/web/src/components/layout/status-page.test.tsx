import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { ServiceUnavailable } from "./status-page";
describe("outage page", () => {
  it("shows the failed request's reference, when there is one", () => {
    render(<ServiceUnavailable requestId="req_2yEHYLwiwPVhLW" />, {
      wrapper: MemoryRouter,
    });
    expect(screen.getByRole("heading")).toHaveTextContent("A brief pause.");
    expect(screen.getByText(/Reference:/)).toHaveTextContent(
      "Reference: req_2yEHYLwiwPVhLW",
    );
  });
  it("shows none for a network failure", () => {
    render(<ServiceUnavailable />, { wrapper: MemoryRouter });
    expect(screen.queryByText(/Reference/)).toBeNull();
  });
});
