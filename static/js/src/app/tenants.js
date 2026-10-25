let tenants = []
let editingId = -1

const loadTenants = () => {
    $("#tenantTable").hide()
    $("#loading").show()
    api.tenants.get()
        .success((ts) => {
            tenants = ts
            $("#loading").hide()
            $("#tenantTable").show()
            const body = $("#tenantTable tbody").empty()
            $.each(tenants, (i, tenant) => {
                const row = $("<tr>")
                row.append($("<td>").text(tenant.name))
                row.append($("<td>").text(moment(tenant.created_at).format("MMMM Do YYYY, h:mm:ss a")))
                const actions = $("<div class='pull-right'>")
                actions.append($("<button class='btn btn-primary edit-button'>").attr("data-tenant-id", tenant.id).html("<i class='fa fa-pencil'></i>"))
                actions.append(" ")
                actions.append($("<button class='btn btn-danger delete-button'>").attr("data-tenant-id", tenant.id).html("<i class='fa fa-trash-o'></i>"))
                row.append($("<td>").append(actions))
                body.append(row)
            })
        })
        .error(() => errorFlash("Error fetching tenants"))
}

const saveTenant = () => {
    const name = $("#name").val()
    if (editingId === -1) {
        api.tenants.post({ name: name })
            .success(() => { successFlash("Tenant created"); $("#modal").modal("hide"); loadTenants() })
            .error((data) => modalError(data.responseJSON.message))
    } else {
        api.tenantId.put({ id: editingId, name: name })
            .success(() => { successFlash("Tenant updated"); $("#modal").modal("hide"); loadTenants() })
            .error((data) => modalError(data.responseJSON.message))
    }
}

const editTenant = (id) => {
    editingId = id
    $("#modal\\.flashes").empty()
    if (id === -1) {
        $("#tenantModalLabel").text("New Tenant")
        $("#name").val("")
    } else {
        const tenant = tenants.find((t) => t.id === id)
        $("#tenantModalLabel").text("Edit Tenant")
        $("#name").val(tenant ? tenant.name : "")
    }
    $("#modal").modal("show")
}

const deleteTenant = (id) => {
    const tenant = tenants.find((t) => t.id === id)
    Swal.fire({
        title: "Are you sure?",
        text: "Delete the tenant " + escapeHtml(tenant.name) + "? Its users and teams must be removed first.",
        type: "warning",
        showCancelButton: true,
        confirmButtonText: "Delete",
        confirmButtonColor: "#428bca",
        reverseButtons: true,
        preConfirm: () => {
            return new Promise((resolve, reject) => {
                api.tenantId.delete(id)
                    .success(() => resolve())
                    .error((data) => reject(data.responseJSON.message))
            }).catch((error) => Swal.showValidationMessage(error))
        }
    }).then((result) => {
        if (result.value) {
            Swal.fire("Tenant Deleted!", "", "success")
            loadTenants()
        }
    })
}

$(document).ready(function () {
    loadTenants()
    $("#new_button").click(() => editTenant(-1))
    $("#modalSubmit").click(saveTenant)
    $("#tenantTable").on("click", ".edit-button", function () { editTenant(parseInt($(this).attr("data-tenant-id"), 10)) })
    $("#tenantTable").on("click", ".delete-button", function () { deleteTenant(parseInt($(this).attr("data-tenant-id"), 10)) })
})
