# Database Gateway Implementation Plan

(Plan content reconstructed from session context - see brief files for task details)

**Goal:** Replace the multi-port database proxy model with a single-port gateway that uses a two-layer auth model.

**Architecture:** Single TCP listener on :33060 auto-detects MySQL vs PostgreSQL. Gateway performs inline protocol MITM to extract username+password, validates against bastion user store, checks RBAC, then connects upstream with stored target credentials.

**Tech Stack:** Go 1.24+, GORM, Vue 3 + Element Plus + TypeScript
