package controllers

import "github.com/gophish/gophish/models"

func modelsGetUser1() (models.User, error) { return models.GetUser(1) }
