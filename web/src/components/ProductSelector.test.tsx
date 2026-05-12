import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";

import ProductSelector from "./ProductSelector";

const products = [
  { pid: 1, name: "Basic", gid: 2, type: "hostingaccount", paytype: "recurring" },
  { pid: 5, name: "Pro", gid: 2, type: "hostingaccount", paytype: "recurring" },
];

describe("ProductSelector", () => {
  it("renders both columns with titles", () => {
    render(<ProductSelector products={products} initialEnabled={[]} onSave={() => {}} />);
    expect(screen.getByText("Available Products")).toBeInTheDocument();
    expect(screen.getByText("Enabled Products")).toBeInTheDocument();
  });

  it("filters Available by search query (case-insensitive)", () => {
    render(<ProductSelector products={products} initialEnabled={[]} onSave={() => {}} />);
    const input = screen.getByPlaceholderText(/Search products/i);
    fireEvent.change(input, { target: { value: "pro" } });
    expect(screen.getByText("Pro")).toBeInTheDocument();
    expect(screen.queryByText("Basic")).not.toBeInTheDocument();
  });

  it("moves a product to Enabled on click", () => {
    render(<ProductSelector products={products} initialEnabled={[]} onSave={() => {}} />);
    fireEvent.click(screen.getByText("Basic"));
    // After click: Basic should be under the Enabled Products column.
    const enabledColumn = screen.getByText("Enabled Products").closest("div")!.parentElement!;
    expect(within(enabledColumn).getByText("Basic")).toBeInTheDocument();
  });

  it("emits save with the comma-separated sorted pid list", () => {
    const onSave = vi.fn();
    render(<ProductSelector products={products} initialEnabled={[5, 1]} onSave={onSave} />);
    fireEvent.click(screen.getByText("Save Changes"));
    expect(onSave).toHaveBeenCalledWith("1,5");
  });

  it("emits empty string when nothing enabled", () => {
    const onSave = vi.fn();
    render(<ProductSelector products={products} initialEnabled={[]} onSave={onSave} />);
    fireEvent.click(screen.getByText("Save Changes"));
    expect(onSave).toHaveBeenCalledWith("");
  });

  it("Enable All moves every product to the Enabled column", () => {
    render(<ProductSelector products={products} initialEnabled={[]} onSave={() => {}} />);
    fireEvent.click(screen.getByText("Enable All"));
    const enabledColumn = screen.getByText("Enabled Products").closest("div")!.parentElement!;
    expect(within(enabledColumn).getByText("Basic")).toBeInTheDocument();
    expect(within(enabledColumn).getByText("Pro")).toBeInTheDocument();
  });

  it("Disable All clears the Enabled column", () => {
    render(<ProductSelector products={products} initialEnabled={[1, 5]} onSave={() => {}} />);
    fireEvent.click(screen.getByText("Disable All"));
    const enabledColumn = screen.getByText("Enabled Products").closest("div")!.parentElement!;
    expect(within(enabledColumn).getByText("No products")).toBeInTheDocument();
  });
});
