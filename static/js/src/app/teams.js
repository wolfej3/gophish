let teams = []
let allUsers = []
let tenantsById = {}
let editingId = -1
let membersTeamId = -1
const isProvider = !!(window.user && window.user.manage_tenants)

const loadTeams = () => {
    $("#teamTable").hide()
    $("#loading").show()
    api.teams.shared()
        .success((ts) => {
            teams = ts
            $("#loading").hide()
            $("#teamTable").show()
            const body = $("#teamTable tbody").empty()
            $.each(teams, (i, team) => {
                const row = $("<tr>")
                row.append($("<td>").text(team.name))
                if (isProvider) {
                    const tenant = tenantsById[team.tenant_id]
                    row.append($("<td class='tenant-col'>").text(tenant ? tenant.name : "—"))
                }
                row.append($("<td>").append($("<button class='btn btn-default btn-sm members-button'>")
                    .attr("data-team-id", team.id)
                    .html("<i class='fa fa-users'></i> Members")))
                const actions = $("<div class='pull-right'>")
                actions.append($("<button class='btn btn-primary edit-button'>").attr("data-team-id", team.id).html("<i class='fa fa-pencil'></i>"))
                actions.append(" ")
                actions.append($("<button class='btn btn-danger delete-button'>").attr("data-team-id", team.id).html("<i class='fa fa-trash-o'></i>"))
                row.append($("<td>").append(actions))
                body.append(row)
            })
        })
        .error(() => errorFlash("Error fetching teams"))
}

const saveTeam = () => {
    const name = $("#name").val()
    if (editingId === -1) {
        const team = { name: name }
        if (isProvider) {
            team.tenant_id = parseInt($("#tenantSelect").val(), 10) || 0
        }
        api.teams.post(team)
            .success(() => { successFlash("Team created"); $("#modal").modal("hide"); loadTeams() })
            .error((data) => modalError(data.responseJSON.message))
    } else {
        api.teamId.put({ id: editingId, name: name })
            .success(() => { successFlash("Team updated"); $("#modal").modal("hide"); loadTeams() })
            .error((data) => modalError(data.responseJSON.message))
    }
}

const editTeam = (id) => {
    editingId = id
    $("#modal\\.flashes").empty()
    if (id === -1) {
        $("#teamModalLabel").text("New Team")
        $("#name").val("")
        // A team's tenant is only chosen at creation, and only by a provider.
        $("#tenantSelectGroup").toggle(isProvider)
    } else {
        const team = teams.find((t) => t.id === id)
        $("#teamModalLabel").text("Edit Team")
        $("#name").val(team ? team.name : "")
        $("#tenantSelectGroup").hide()
    }
    $("#modal").modal("show")
}

const deleteTeam = (id) => {
    const team = teams.find((t) => t.id === id)
    Swal.fire({
        title: "Are you sure?",
        text: "Delete the team " + escapeHtml(team.name) + "?",
        type: "warning",
        showCancelButton: true,
        confirmButtonText: "Delete",
        confirmButtonColor: "#428bca",
        reverseButtons: true,
        preConfirm: () => {
            return new Promise((resolve, reject) => {
                api.teamId.delete(id)
                    .success(() => resolve())
                    .error((data) => reject(data.responseJSON.message))
            }).catch((error) => Swal.showValidationMessage(error))
        }
    }).then((result) => {
        if (result.value) {
            Swal.fire("Team Deleted!", "", "success")
            loadTeams()
        }
    })
}

const showMembers = (id) => {
    membersTeamId = id
    const team = teams.find((t) => t.id === id)
    $("#membersTeamName").text(team ? team.name : "")
    $("#members\\.flashes").empty()
    loadMembers()
    $("#membersModal").modal("show")
}

const loadMembers = () => {
    api.teamId.members(membersTeamId)
        .success((members) => {
            const body = $("#membersBody").empty()
            const memberIds = members.map((m) => m.user_id)
            $.each(members, (i, m) => {
                const row = $("<tr>")
                row.append($("<td>").text(m.username))
                const roleSelect = $("<select class='form-control input-sm member-role'>")
                    .attr("data-user-id", m.user_id)
                    .append($("<option value='editor'>").text("Editor").prop("selected", m.role === "editor"))
                    .append($("<option value='viewer'>").text("Viewer").prop("selected", m.role === "viewer"))
                row.append($("<td>").append(roleSelect))
                row.append($("<td>").append($("<button class='btn btn-danger btn-sm remove-member'>")
                    .attr("data-user-id", m.user_id).html("<i class='fa fa-trash-o'></i>")))
                body.append(row)
            })
            // The add dropdown offers users who aren't members yet
            const select = $("#addMemberUser").empty()
            $.each(allUsers, (i, u) => {
                if (memberIds.indexOf(u.id) === -1) {
                    select.append($("<option>").val(u.id).text(u.username))
                }
            })
        })
        .error(() => errorFlash("Error fetching members"))
}

const addMember = () => {
    const uid = parseInt($("#addMemberUser").val(), 10)
    if (!uid) {
        return
    }
    api.teamId.addMember(membersTeamId, { user_id: uid, role: $("#addMemberRole").val() })
        .success(() => loadMembers())
        .error((data) => errorFlash(data.responseJSON.message))
}

$(document).ready(function () {
    // The member dropdown needs the full user list
    api.users.get()
        .success((us) => { allUsers = us })
        .error(() => {})
    // Provider admins see a Tenant column and pick a tenant when creating a team.
    if (isProvider) {
        $(".tenant-col").show()
        api.tenants.get()
            .success((ts) => {
                const select = $("#tenantSelect").empty()
                $.each(ts, (i, t) => {
                    tenantsById[t.id] = t
                    select.append($("<option>").val(t.id).text(t.name))
                })
                loadTeams()
            })
            .error(() => loadTeams())
    } else {
        loadTeams()
    }
    $("#new_button").click(() => editTeam(-1))
    $("#modalSubmit").click(saveTeam)
    $("#teamTable").on("click", ".edit-button", function () { editTeam(parseInt($(this).attr("data-team-id"), 10)) })
    $("#teamTable").on("click", ".delete-button", function () { deleteTeam(parseInt($(this).attr("data-team-id"), 10)) })
    $("#teamTable").on("click", ".members-button", function () { showMembers(parseInt($(this).attr("data-team-id"), 10)) })
    $("#addMemberButton").click(addMember)
    $("#membersBody").on("change", ".member-role", function () {
        api.teamId.addMember(membersTeamId, { user_id: parseInt($(this).attr("data-user-id"), 10), role: $(this).val() })
            .success(() => successFlashFade("Role updated", 1))
            .error((data) => errorFlash(data.responseJSON.message))
    })
    $("#membersBody").on("click", ".remove-member", function () {
        api.teamId.removeMember(membersTeamId, parseInt($(this).attr("data-user-id"), 10))
            .success(() => loadMembers())
            .error((data) => errorFlash(data.responseJSON.message))
    })
})
