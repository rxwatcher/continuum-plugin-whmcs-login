import { Routes, Route, Navigate } from "react-router";
import Layout from "./components/Layout";
import ProductsPage from "./pages/Products";
import SettingsPage from "./pages/Settings";
import DiagnosticsPage from "./pages/Diagnostics";
import { Toaster } from "./components/ui/sonner";

export default function App() {
  return (
    <>
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<Navigate to="products" replace />} />
          <Route path="products" element={<ProductsPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="diagnostics" element={<DiagnosticsPage />} />
          <Route path="*" element={<Navigate to="products" replace />} />
        </Route>
      </Routes>
      <Toaster />
    </>
  );
}
