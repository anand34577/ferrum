import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { ColumnDef } from "@tanstack/react-table"
import { Pencil, Plus, Trash2, Users } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"
import { useFormDirty } from "@/components/settings/use-form-dirty"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { DataTable } from "@/components/ui/data-table"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { ErrorState } from "@/components/ui/error-state"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Hint } from "@/components/ui/tooltip"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"

interface FerrumUser {
  id: string
  username: string
  email: string
  isAdmin: boolean
  createdAt: string
  roles: string[] | null
}

interface Role {
  id: string
  name: string
  description?: string
  color?: string
  isSystem: boolean
}

// The create form's pristine state — its dirty baseline. Shared by the
// useState initializer and the post-create reset so the two can't drift.
const CREATE_FORM_INITIAL = { username: "", email: "", password: "", isAdmin: false, roleId: "" }

export function UsersPage() {
  const { user: me } = useAuth()
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState(CREATE_FORM_INITIAL)
  // Non-remounting form: dirty clears because the post-create reset restores
  // the pristine state below. Cancel deliberately keeps typed values (the
  // next open re-initializes both, so dirty can't go stale).
  const createDirty = useFormDirty(form, CREATE_FORM_INITIAL)

  const usersQuery = useQuery({ queryKey: ["users"], queryFn: () => api.get<FerrumUser[]>("/users/"), refetchInterval: 30_000 })
  const rolesQuery = useQuery({ queryKey: ["roles"], queryFn: () => api.get<Role[]>("/roles") })

  const createMutation = useMutation({
    mutationFn: () => api.post("/users/", form),
    onSuccess: () => {
      toast.success("User created")
      queryClient.invalidateQueries({ queryKey: ["users"] })
      setForm(CREATE_FORM_INITIAL)
      setShowForm(false)
    },
    // Failure surfaces inline via <FormError>, not a toast — it has to stay
    // readable next to the fields being corrected.
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/users/${id}`),
    onSuccess: () => {
      toast.success("User removed")
      queryClient.invalidateQueries({ queryKey: ["users"] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove user"),
  })

  async function removeUser(u: FerrumUser) {
    const ok = await confirm({
      title: `Remove ${u.username}?`,
      description: "Their sessions are revoked immediately and the account cannot sign in. This cannot be undone.",
      confirmLabel: "Remove",
    })
    if (ok) deleteMutation.mutate(u.id)
  }

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [editTarget, setEditTarget] = useState<FerrumUser | null>(null)
  const [editForm, setEditForm] = useState({ email: "", password: "", isAdmin: false })
  // The edit form's own starting state (password always starts blank — the
  // API omits stored credentials), re-derived whenever openEdit sets a new
  // target. While editTarget is null the form isn't rendered, so the
  // all-blank placeholder baseline is never compared against anything real.
  const editInitial = editTarget
    ? { email: editTarget.email, password: "", isAdmin: editTarget.isAdmin }
    : { email: "", password: "", isAdmin: false }
  const editDirty = useFormDirty(editForm, editInitial)

  const updateMutation = useMutation({
    mutationFn: (input: { id: string; email?: string; password?: string; isAdmin?: boolean }) => {
      const { id, ...body } = input
      return api.put(`/users/${id}`, body)
    },
    onSuccess: () => {
      toast.success("User updated")
      queryClient.invalidateQueries({ queryKey: ["users"] })
      setEditTarget(null)
    },
    // Failure surfaces inline via <FormError>, not a toast.
  })

  function openEdit(u: FerrumUser) {
    setEditTarget(u)
    setEditForm({ email: u.email, password: "", isAdmin: u.isAdmin })
  }

  function submitEdit() {
    if (!editTarget) return
    const body: { email?: string; password?: string; isAdmin?: boolean } = { isAdmin: editForm.isAdmin }
    if (editForm.email !== editTarget.email) body.email = editForm.email
    if (editForm.password) body.password = editForm.password
    updateMutation.mutate({ id: editTarget.id, ...body })
  }
  const bulkDeleteMutation = useMutation({
    mutationFn: async (ids: string[]) => {
      const targets = ids.filter((id) => id !== me?.id) // never let a bulk action remove your own account
      const results = await Promise.allSettled(targets.map((id) => api.delete(`/users/${id}`)))
      return { total: targets.length, failed: results.filter((r) => r.status === "rejected").length }
    },
    onSuccess: ({ total, failed }) => {
      if (failed === 0) toast.success(`${total} user${total === 1 ? "" : "s"} removed`)
      else toast.error(`${failed} of ${total} removals failed`)
      queryClient.invalidateQueries({ queryKey: ["users"] })
      setSelected(new Set())
    },
    onError: () => toast.error("Bulk removal failed"),
  })

  async function bulkRemove(ids: string[]) {
    const actionable = ids.filter((id) => id !== me?.id)
    if (actionable.length === 0) return
    const ok = await confirm({
      title: `Remove ${actionable.length} user${actionable.length === 1 ? "" : "s"}?`,
      description: "All selected accounts are removed and signed out. This cannot be undone.",
      confirmLabel: "Remove all",
    })
    if (ok) bulkDeleteMutation.mutate(ids)
  }

  const columns = useMemo<ColumnDef<FerrumUser>[]>(
    () => [
      {
        accessorKey: "username",
        header: "User",
        cell: (c) => (
          <div>
            <p className="font-medium">{c.row.original.username}</p>
            <p className="text-xs text-[var(--text-muted)]">{c.row.original.email}</p>
          </div>
        ),
      },
      {
        id: "roles",
        header: "Roles",
        cell: (c) => (
          <div className="flex flex-wrap gap-1">
            {c.row.original.isAdmin && <Badge variant="brand">Admin</Badge>}
            {c.row.original.roles?.map((r) => <Badge key={r}>{r}</Badge>)}
          </div>
        ),
      },
      {
        accessorKey: "createdAt",
        header: "Created",
        meta: { hideBelowMd: true },
        cell: (c) => <span className="text-xs text-[var(--text-muted)] tabular">{new Date(c.getValue<string>()).toLocaleDateString()}</span>,
      },
      {
        id: "actions",
        header: "",
        cell: (c) => (
          <div className="flex items-center justify-end gap-1">
          <Hint label="Edit user">
            <Button
              size="icon"
              variant="ghost"
              aria-label={`Edit ${c.row.original.username}`}
              onClick={() => openEdit(c.row.original)}
            >
              <Pencil className="h-4 w-4" />
            </Button>
          </Hint>
          <Hint label="Remove user">
            <Button
              size="icon"
              variant="ghost-danger"
              disabled={c.row.original.id === me?.id}
              aria-label={`Remove ${c.row.original.username}`}
              onClick={() => removeUser(c.row.original)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </Hint>
          </div>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [me],
  )

  return (
    <div className="space-y-4">
      <PageHeader
        title="Users"
        description="Local accounts with access to this Ferrum instance."
        icon={Users}
        onRefresh={() => void queryClient.invalidateQueries({ queryKey: ["users"] })}
        refreshing={usersQuery.isRefetching}
        actions={
          <Button onClick={() => setShowForm((s) => !s)}>
            <Plus className="h-4 w-4" /> Add user
          </Button>
        }
      />

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle>New user</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label>Username</Label>
                <Input
                  value={form.username}
                  onChange={(e) => setForm({ ...form, username: e.target.value })}
                  autoComplete="off"
                  aria-required="true"
                />
              </div>
              <div className="space-y-1.5">
                <Label>Email</Label>
                <Input
                  type="email"
                  value={form.email}
                  onChange={(e) => setForm({ ...form, email: e.target.value })}
                  autoComplete="off"
                  aria-required="true"
                />
              </div>
              <div className="space-y-1.5">
                <Label>Password</Label>
                <Input
                  type="password"
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  autoComplete="new-password"
                  aria-required="true"
                  aria-describedby="password-hint"
                />
              </div>
              <div className="space-y-1.5">
                <Label>Role</Label>
                <Select value={form.roleId} onValueChange={(v) => setForm({ ...form, roleId: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder="No role" />
                  </SelectTrigger>
                  <SelectContent>
                    {rolesQuery.data?.map((r) => (
                      <SelectItem key={r.id} value={r.id}>{r.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-[var(--text-faint)]">Roles are organizational labels — effective access is admin vs non-admin (the switch below).</p>
              </div>
            </div>
            <p id="password-hint" className="text-xs text-[var(--text-faint)]">Minimum 8 characters.</p>
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={form.isAdmin} onCheckedChange={(v) => setForm({ ...form, isAdmin: v })} />
              Grant full admin access
            </label>
            <FormError
              message={
                createMutation.error instanceof ApiError
                  ? createMutation.error.message
                  : createMutation.error
                    ? "Couldn't create the user — try again."
                    : null
              }
            />
            {(!form.username || !form.email || form.password.length < 8) && (
              <p className="text-xs text-[var(--text-muted)]">Username, email and a password of at least 8 characters are required.</p>
            )}
            <div className="flex gap-2">
              <Button
                onClick={() => createMutation.mutate()}
                loading={createMutation.isPending}
                disabled={!createDirty || !form.username || !form.email || form.password.length < 8}
              >
                Create user
              </Button>
              <Button variant="ghost" onClick={() => setShowForm(false)}>Cancel</Button>
            </div>
          </CardContent>
        </Card>
      )}

      {editTarget && (
        <Card>
          <CardHeader>
            <CardTitle>Edit {editTarget.username}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="edit-email">Email</Label>
                <Input
                  id="edit-email"
                  type="email"
                  value={editForm.email}
                  onChange={(e) => setEditForm({ ...editForm, email: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-password">New password</Label>
                <Input
                  id="edit-password"
                  type="password"
                  minLength={8}
                  autoComplete="new-password"
                  value={editForm.password}
                  onChange={(e) => setEditForm({ ...editForm, password: e.target.value })}
                  placeholder="Leave blank to keep current"
                  aria-describedby="edit-password-hint"
                />
                <p id="edit-password-hint" className="text-xs text-[var(--text-faint)]">
                  Resetting the password signs this user out everywhere.
                </p>
              </div>
            </div>
            {editTarget.id !== me?.id && (
              <label className="flex items-center gap-2 text-sm">
                <Switch
                  checked={editForm.isAdmin}
                  onCheckedChange={(v) => setEditForm({ ...editForm, isAdmin: v })}
                />
                Grant full admin access
              </label>
            )}
            <FormError
              message={
                updateMutation.error instanceof ApiError
                  ? updateMutation.error.message
                  : updateMutation.error
                    ? "Couldn't update the user — try again."
                    : null
              }
            />
            <div className="flex gap-2">
              <Button onClick={submitEdit} loading={updateMutation.isPending} disabled={!editDirty}>
                Save changes
              </Button>
              <Button variant="ghost" onClick={() => setEditTarget(null)}>
                Cancel
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {usersQuery.isError ? (
        <ErrorState title="Couldn't load users" onRetry={usersQuery.refetch} />
      ) : (
        <Card>
          <CardContent className="pt-4">
            <DataTable
              columns={columns}
              data={usersQuery.data ?? []}
              loading={usersQuery.isLoading}
              searchPlaceholder="Search users..."
              emptyMessage="No users yet — add the first teammate."
              selection={{
                rowId: (u) => u.id,
                selected,
                onSelectedChange: setSelected,
                bulkActions: (ids) => (
                  <Button
                    size="sm"
                    variant="destructive"
                    loading={bulkDeleteMutation.isPending}
                    onClick={() => bulkRemove(ids)}
                  >
                    <Trash2 className="h-3.5 w-3.5" /> Remove selected
                  </Button>
                ),
              }}
            />
          </CardContent>
        </Card>
      )}
    </div>
  )
}
