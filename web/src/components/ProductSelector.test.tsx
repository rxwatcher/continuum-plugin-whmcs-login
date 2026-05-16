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
    expect(screen.getByText("Available products")).toBeInTheDocument();
    expect(screen.getByText("Allowed products")).toBeInTheDocument();
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
    // After click: Basic should be under the Allowed products column.
    const enabledColumn = screen.getByText("Allowed products").closest("div")!.parentElement!;
    expect(within(enabledColumn).getByText("Basic")).toBeInTheDocument();
  });

  it("emits save with the comma-separated sorted pid list", () => {
    const onSave = vi.fn();
    render(<ProductSelector products={products} initialEnabled={[5, 1]} onSave={onSave} />);
    fireEvent.click(screen.getByText("Save product access"));
    expect(onSave).toHaveBeenCalledWith("1,5");
  });

  it("emits empty string when nothing enabled", () => {
    const onSave = vi.fn();
    render(<ProductSelector products={products} initialEnabled={[]} onSave={onSave} />);
    fireEvent.click(screen.getByText("Save product access"));
    expect(onSave).toHaveBeenCalledWith("");
  });

  it("Select all moves every product to the Allowed column", () => {
    render(<ProductSelector products={products} initialEnabled={[]} onSave={() => {}} />);
    fireEvent.click(screen.getByText("Select all"));
    const enabledColumn = screen.getByText("Allowed products").closest("div")!.parentElement!;
    expect(within(enabledColumn).getByText("Basic")).toBeInTheDocument();
    expect(within(enabledColumn).getByText("Pro")).toBeInTheDocument();
  });

  it("Clear clears the Allowed column", () => {
    render(<ProductSelector products={products} initialEnabled={[1, 5]} onSave={() => {}} />);
    fireEvent.click(screen.getByText("Clear"));
    const enabledColumn = screen.getByText("Allowed products").closest("div")!.parentElement!;
    expect(within(enabledColumn).getByText("No products")).toBeInTheDocument();
  });
});
